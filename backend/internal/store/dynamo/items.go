package dynamo

// Item (record) shapes for marshalling model types onto the single table via
// feature/dynamodb/attributevalue. Records use string timestamps (RFC3339Nano)
// rather than embedding time.Time directly, since attributevalue's default
// reflection-based encoding doesn't understand time.Time's unexported fields.

import (
	"fmt"
	"time"

	"github.com/dgarwin/alertme/backend/internal/model"
)

// ttlRetention is how long page and event items live before DynamoDB TTL
// purges them (PLAN-AWS.md §1, "auto-purge after retention window").
const ttlRetention = 90 * 24 * time.Hour

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}

type userRecord struct {
	PK          string `dynamodbav:"PK"`
	SK          string `dynamodbav:"SK"`
	ID          string `dynamodbav:"id"`
	DisplayName string `dynamodbav:"display_name"`
	CreatedAt   string `dynamodbav:"created_at"`
}

func newUserRecord(u model.User) userRecord {
	return userRecord{
		PK:          userPK(u.ID),
		SK:          skProfile,
		ID:          u.ID,
		DisplayName: u.DisplayName,
		CreatedAt:   fmtTime(u.CreatedAt),
	}
}

func (r userRecord) toModel() (model.User, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return model.User{}, fmt.Errorf("user %s: %w", r.ID, err)
	}
	return model.User{ID: r.ID, DisplayName: r.DisplayName, CreatedAt: created}, nil
}

type deviceRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	UserID    string `dynamodbav:"user_id"`
	Token     string `dynamodbav:"token"`
	Platform  string `dynamodbav:"platform"`
	UpdatedAt string `dynamodbav:"updated_at"`
}

func newDeviceRecord(d model.Device) deviceRecord {
	return deviceRecord{
		PK:        userPK(d.UserID),
		SK:        devicSK(d.Token),
		UserID:    d.UserID,
		Token:     d.Token,
		Platform:  d.Platform,
		UpdatedAt: fmtTime(d.UpdatedAt),
	}
}

func (r deviceRecord) toModel() (model.Device, error) {
	updated, err := parseTime(r.UpdatedAt)
	if err != nil {
		return model.Device{}, fmt.Errorf("device %s: %w", r.Token, err)
	}
	return model.Device{UserID: r.UserID, Token: r.Token, Platform: r.Platform, UpdatedAt: updated}, nil
}

type pairingRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	UserID    string `dynamodbav:"user_id"`
	PeerID    string `dynamodbav:"peer_id"`
	PeerName  string `dynamodbav:"peer_name"`
	Status    string `dynamodbav:"status"`
	CreatedAt string `dynamodbav:"created_at"`
}

func newPairingRecord(p model.Pairing) pairingRecord {
	return pairingRecord{
		PK:        userPK(p.UserID),
		SK:        pairSK(p.PeerID),
		UserID:    p.UserID,
		PeerID:    p.PeerID,
		PeerName:  p.PeerName,
		Status:    string(p.Status),
		CreatedAt: fmtTime(p.CreatedAt),
	}
}

func (r pairingRecord) toModel() (model.Pairing, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return model.Pairing{}, fmt.Errorf("pairing %s/%s: %w", r.UserID, r.PeerID, err)
	}
	return model.Pairing{
		UserID:    r.UserID,
		PeerID:    r.PeerID,
		PeerName:  r.PeerName,
		Status:    model.PairingStatus(r.Status),
		CreatedAt: created,
	}, nil
}

type inviteRecord struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	Code      string `dynamodbav:"code"`
	CreatorID string `dynamodbav:"creator_id"`
	ExpiresAt string `dynamodbav:"expires_at"`
	TTL       int64  `dynamodbav:"ttl"`
}

func newInviteRecord(inv model.Invite) inviteRecord {
	return inviteRecord{
		PK:        invitePK(inv.Code),
		SK:        skMeta,
		Code:      inv.Code,
		CreatorID: inv.CreatorID,
		ExpiresAt: fmtTime(inv.ExpiresAt),
		TTL:       inv.ExpiresAt.Unix(),
	}
}

func (r inviteRecord) toModel() (model.Invite, error) {
	expires, err := parseTime(r.ExpiresAt)
	if err != nil {
		return model.Invite{}, fmt.Errorf("invite %s: %w", r.Code, err)
	}
	return model.Invite{Code: r.Code, CreatorID: r.CreatorID, ExpiresAt: expires}, nil
}

type pageRecord struct {
	PK             string `dynamodbav:"PK"`
	SK             string `dynamodbav:"SK"`
	ID             string `dynamodbav:"id"`
	SenderID       string `dynamodbav:"sender_id"`
	RecipientID    string `dynamodbav:"recipient_id"`
	Message        string `dynamodbav:"message"`
	State          string `dynamodbav:"state"`
	Attempt        int    `dynamodbav:"attempt"`
	IdempotencyKey string `dynamodbav:"idempotency_key"`
	CreatedAt      string `dynamodbav:"created_at"`
	ExpiresAt      string `dynamodbav:"expires_at"`
	AckedAt        string `dynamodbav:"acked_at,omitempty"`
	AckNote        string `dynamodbav:"ack_note,omitempty"`
	TTL            int64  `dynamodbav:"ttl"`
	GSI1PK         string `dynamodbav:"GSI1PK"`
	GSI1SK         string `dynamodbav:"GSI1SK"`
	GSI2PK         string `dynamodbav:"GSI2PK"`
	GSI2SK         string `dynamodbav:"GSI2SK"`
}

func newPageRecord(p model.Page) pageRecord {
	var ackedAt string
	if p.AckedAt != nil {
		ackedAt = fmtTime(*p.AckedAt)
	}
	return pageRecord{
		PK:             pagePK(p.ID),
		SK:             skMeta,
		ID:             p.ID,
		SenderID:       p.SenderID,
		RecipientID:    p.RecipientID,
		Message:        p.Message,
		State:          string(p.State),
		Attempt:        p.Attempt,
		IdempotencyKey: p.IdempotencyKey,
		CreatedAt:      fmtTime(p.CreatedAt),
		ExpiresAt:      fmtTime(p.ExpiresAt),
		AckedAt:        ackedAt,
		AckNote:        p.AckNote,
		TTL:            p.CreatedAt.Add(ttlRetention).Unix(),
		GSI1PK:         userPK(p.RecipientID),
		GSI1SK:         pageGSI1SK(p.CreatedAt, p.ID),
		GSI2PK:         userPK(p.SenderID),
		GSI2SK:         pageGSI2SK(p.CreatedAt, p.ID),
	}
}

func (r pageRecord) toModel() (model.Page, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return model.Page{}, fmt.Errorf("page %s: %w", r.ID, err)
	}
	expires, err := parseTime(r.ExpiresAt)
	if err != nil {
		return model.Page{}, fmt.Errorf("page %s: %w", r.ID, err)
	}
	p := model.Page{
		ID:             r.ID,
		SenderID:       r.SenderID,
		RecipientID:    r.RecipientID,
		Message:        r.Message,
		State:          model.PageState(r.State),
		Attempt:        r.Attempt,
		IdempotencyKey: r.IdempotencyKey,
		CreatedAt:      created,
		ExpiresAt:      expires,
		AckNote:        r.AckNote,
	}
	if r.AckedAt != "" {
		at, err := parseTime(r.AckedAt)
		if err != nil {
			return model.Page{}, fmt.Errorf("page %s: %w", r.ID, err)
		}
		p.AckedAt = &at
	}
	return p, nil
}

type eventRecord struct {
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	PageID string `dynamodbav:"page_id"`
	Type   string `dynamodbav:"type"`
	Detail string `dynamodbav:"detail,omitempty"`
	At     string `dynamodbav:"at"`
	TTL    int64  `dynamodbav:"ttl"`
}

func newEventRecord(e model.PageEvent) eventRecord {
	return eventRecord{
		PK:     pagePK(e.PageID),
		SK:     eventSK(e.At, e.Type),
		PageID: e.PageID,
		Type:   e.Type,
		Detail: e.Detail,
		At:     fmtTime(e.At),
		TTL:    e.At.Add(ttlRetention).Unix(),
	}
}

func (r eventRecord) toModel() (model.PageEvent, error) {
	at, err := parseTime(r.At)
	if err != nil {
		return model.PageEvent{}, fmt.Errorf("event %s/%s: %w", r.PageID, r.Type, err)
	}
	return model.PageEvent{PageID: r.PageID, Type: r.Type, Detail: r.Detail, At: at}, nil
}

// idemRecord is the idempotency guard: PK=IDEM#<key>, pointing at the page it
// guards so a replayed create can look the original page back up.
type idemRecord struct {
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	PageID string `dynamodbav:"page_id"`
}
