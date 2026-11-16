package mailwatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/store/fake"
)

type fakeMailbox struct{ msgs []Message }

func (f *fakeMailbox) UnreadFrom(context.Context, []string) ([]Message, error) { return f.msgs, nil }

func newWatcher(t *testing.T, mb Mailbox) (*Watcher, *fake.Store, *fake.Enqueuer) {
	t.Helper()
	st := fake.New()
	if err := st.PutUser(context.Background(), model.User{ID: "david", DisplayName: "david", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	q := &fake.Enqueuer{}
	return &Watcher{
		Store: st, Queue: q, Mail: mb,
		Senders:     []string{"a@example.com"},
		RecipientID: "david", BotID: "bot-mailwatch", BotName: "AlertMe Mail",
		Now: time.Now,
	}, st, q
}

func TestRun_PairsOncePagesOncePerMessage(t *testing.T) {
	ctx := context.Background()
	mb := &fakeMailbox{msgs: []Message{{ID: "m1", From: "A <a@example.com>", Subject: "Gig tonight"}}}
	w, st, q := newWatcher(t, mb)

	n, err := w.Run(ctx)
	if err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v", n, err)
	}
	p, err := st.GetPairing(ctx, "david", "bot-mailwatch")
	if err != nil || p.Status != model.PairingActive || p.PeerName != "AlertMe Mail" {
		t.Fatalf("recipient pairing: %+v %v", p, err)
	}
	if len(q.Tasks) != 1 || q.Tasks[0].Task.Attempt != 0 {
		t.Fatalf("expected one attempt-0 task, got %+v", q.Tasks)
	}

	// Same message still unread on the next run: no new page.
	n, err = w.Run(ctx)
	if err != nil || n != 0 || len(q.Tasks) != 1 {
		t.Fatalf("second run: n=%d err=%v tasks=%d", n, err, len(q.Tasks))
	}

	// A new message pages again.
	mb.msgs = append(mb.msgs, Message{ID: "m2", From: "a@example.com", Subject: "Update"})
	if n, err = w.Run(ctx); err != nil || n != 1 {
		t.Fatalf("third run: n=%d err=%v", n, err)
	}
}

func TestRun_RespectsBlock(t *testing.T) {
	ctx := context.Background()
	w, st, q := newWatcher(t, &fakeMailbox{})
	if _, err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.BlockPairing(ctx, "david", "bot-mailwatch"); err != nil {
		t.Fatal(err)
	}
	w.Mail = &fakeMailbox{msgs: []Message{{ID: "m1", From: "a@example.com", Subject: "x"}}}
	if _, err := w.Run(ctx); err == nil && len(q.Tasks) > 0 {
		t.Fatalf("paged despite block: %+v", q.Tasks)
	}
}

func TestFormatMessage_Truncates(t *testing.T) {
	m := FormatMessage(Message{From: "x@y.z", Subject: strings.Repeat("é", 400)})
	if len(m) > maxMessageLen || !strings.HasSuffix(m, "…") {
		t.Fatalf("len=%d suffix ok=%v", len(m), strings.HasSuffix(m, "…"))
	}
	if got := FormatMessage(Message{From: "x@y.z"}); got != "Email from x@y.z: (no subject)" {
		t.Fatalf("got %q", got)
	}
}

func TestQuery(t *testing.T) {
	got := Query([]string{"a@x.com", " b@y.com"}, "2d")
	if got != "is:unread {from:a@x.com from:b@y.com} newer_than:2d" {
		t.Fatalf("got %q", got)
	}
}

func TestGmail_UnreadFrom(t *testing.T) {
	var sawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "rt" || r.Form.Get("grant_type") != "refresh_token" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "at"})
		case r.Header.Get("Authorization") != "Bearer at":
			http.Error(w, "unauth", 401)
		case r.URL.Path == "/users/me/messages":
			sawQuery = r.URL.Query().Get("q")
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"}]}`))
		case r.URL.Path == "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","payload":{"headers":[{"name":"From","value":"Roro <roro@example.com>"},{"name":"Subject","value":"Hi"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := &Gmail{Cred: GmailCredential{ClientID: "c", ClientSecret: "s", RefreshToken: "rt"},
		TokenURL: srv.URL + "/token", APIBase: srv.URL, Window: "2d"}
	msgs, err := g.UnreadFrom(context.Background(), []string{"roro@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m1" || msgs[0].Subject != "Hi" || !strings.Contains(msgs[0].From, "roro@") {
		t.Fatalf("got %+v", msgs)
	}
	if sawQuery != "is:unread {from:roro@example.com} newer_than:2d" {
		t.Fatalf("query %q", sawQuery)
	}

	g.Cred.RefreshToken = "bad"
	if _, err := g.UnreadFrom(context.Background(), []string{"x"}); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("expected invalid_grant error, got %v", err)
	}

	g.Cred.RefreshToken = ""
	if _, err := g.UnreadFrom(context.Background(), nil); err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}
