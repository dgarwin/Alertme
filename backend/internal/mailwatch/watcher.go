// Package mailwatch turns matching unread emails into pages. A scheduled
// lambda (cmd/mailwatch) runs Watcher.Run every few minutes: it asks the
// Mailbox for unread mail from a fixed set of senders and creates one page
// per message, from a bot user, to a single recipient.
//
// Each page's idempotency key is derived from the mail provider's message
// id, and the store's idempotency guard never expires, so a message that
// stays unread across many runs pages exactly once.
package mailwatch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/nag"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store"
)

// maxMessageLen mirrors the API's limit on page messages (bytes).
const maxMessageLen = 500

// Message is the subset of an email the watcher needs.
type Message struct {
	ID      string // provider message id (stable across runs)
	From    string
	Subject string
}

// Mailbox lists unread messages from any of the given senders.
type Mailbox interface {
	UnreadFrom(ctx context.Context, senders []string) ([]Message, error)
}

// ErrNotConfigured means the mailbox credential hasn't been provisioned yet;
// the lambda treats it as a no-op rather than a failure.
var ErrNotConfigured = errors.New("mailbox credential not configured")

type Watcher struct {
	Store       store.Store
	Queue       queue.Enqueuer
	Mail        Mailbox
	Senders     []string
	RecipientID string // user id (Cognito sub) to page
	BotID       string // synthetic sender user id
	BotName     string // display name the recipient sees
	Now         func() time.Time
}

// Run pages the recipient once per matching unread message and returns how
// many new pages were created.
func (w *Watcher) Run(ctx context.Context) (int, error) {
	if err := w.ensurePairing(ctx); err != nil {
		return 0, err
	}
	msgs, err := w.Mail.UnreadFrom(ctx, w.Senders)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, m := range msgs {
		ok, err := w.page(ctx, m)
		if err != nil {
			return created, fmt.Errorf("page for message %s: %w", m.ID, err)
		}
		if ok {
			created++
		}
	}
	return created, nil
}

// ensurePairing makes sure the bot has an active pairing with the recipient,
// creating it on first run via the normal invite/accept store path. If the
// recipient has blocked the bot, it stops: blocking must be respected.
func (w *Watcher) ensurePairing(ctx context.Context) error {
	// The recipient blocking the bot flips only the recipient's copy.
	if rp, err := w.Store.GetPairing(ctx, w.RecipientID, w.BotID); err == nil && rp.Status != model.PairingActive {
		return fmt.Errorf("recipient has %s the %s bot; not paging", rp.Status, w.BotName)
	}
	p, err := w.Store.GetPairing(ctx, w.BotID, w.RecipientID)
	switch {
	case err == nil && p.Status == model.PairingActive:
		return nil
	case err == nil:
		return fmt.Errorf("recipient has %s the %s bot; not paging", p.Status, w.BotName)
	case !errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("get pairing: %w", err)
	}

	recipient, err := w.Store.GetUser(ctx, w.RecipientID)
	if err != nil {
		return fmt.Errorf("recipient %s must have signed in to the app at least once: %w", w.RecipientID, err)
	}
	now := w.Now()
	if err := w.Store.PutUser(ctx, model.User{ID: w.BotID, DisplayName: w.BotName, CreatedAt: now}); err != nil {
		return fmt.Errorf("put bot user: %w", err)
	}
	code := "MAILBOT-" + randomHex(8)
	if err := w.Store.CreateInvite(ctx, model.Invite{Code: code, CreatorID: w.BotID, ExpiresAt: now.Add(10 * time.Minute)}); err != nil {
		return fmt.Errorf("create bot invite: %w", err)
	}
	if _, err := w.Store.AcceptInvite(ctx, code, recipient); err != nil {
		return fmt.Errorf("pair bot with recipient: %w", err)
	}
	log.Printf("paired %s bot with recipient %s", w.BotName, w.RecipientID)
	return nil
}

// page creates and schedules one page; false means this message already
// paged on an earlier run.
func (w *Watcher) page(ctx context.Context, m Message) (bool, error) {
	now := w.Now()
	p := model.Page{
		ID:             randomHex(16),
		SenderID:       w.BotID,
		RecipientID:    w.RecipientID,
		Message:        FormatMessage(m),
		State:          model.PageCreated,
		Attempt:        -1, // see handleCreatePage: first push is attempt 0
		IdempotencyKey: "mail:" + m.ID,
		CreatedAt:      now,
		ExpiresAt:      now.Add(nag.Expiry),
	}
	if err := w.Store.CreatePage(ctx, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return false, nil
		}
		return false, err
	}
	if err := w.Queue.Enqueue(ctx, queue.PageTask{PageID: p.ID, Attempt: 0}, 0); err != nil {
		return false, fmt.Errorf("page %s created but not scheduled: %w", p.ID, err)
	}
	log.Printf("paged %s for mail %s from %s", w.RecipientID, m.ID, m.From)
	return true, nil
}

// FormatMessage renders the page text, truncated to the API's byte limit
// on a rune boundary.
func FormatMessage(m Message) string {
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	s := fmt.Sprintf("Email from %s: %s", strings.TrimSpace(m.From), subject)
	if len(s) <= maxMessageLen {
		return s
	}
	cut := maxMessageLen - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
