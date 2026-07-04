package api

// Handler tests exercise the real Router (and therefore withIdentity's
// DEV_USER_ID fallback, since no algnhsa/API-Gateway context is present
// under httptest) against the in-memory fake store, so they cover both the
// handlers and the identity middleware's local-dev path.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/store/fake"
)

func jsonBody(v any) *bytes.Buffer {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bytes.NewBuffer(b)
}

// seedActivePairing sets up two users and an active pairing between them via
// the real invite/accept flow, so GetPairing(senderID, recipientID) — the
// exact call handleCreatePage makes — returns an active pairing.
func seedActivePairing(t *testing.T, st *fake.Store, senderID, recipientID string) {
	t.Helper()
	ctx := context.Background()
	if err := st.PutUser(ctx, model.User{ID: senderID, DisplayName: senderID, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutUser(ctx, model.User{ID: recipientID, DisplayName: recipientID, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	inv := model.Invite{Code: "CODE-" + senderID + "-" + recipientID, CreatorID: senderID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.CreateInvite(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptInvite(ctx, inv.Code, model.User{ID: recipientID, DisplayName: recipientID, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

type pageEnvelope struct {
	Page model.Page `json:"page"`
}

func TestHandleCreatePage_HappyPath(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	seedActivePairing(t, st, "sender1", "recipient1")
	t.Setenv("DEV_USER_ID", "sender1")

	req := httptest.NewRequest(http.MethodPost, "/pages", jsonBody(map[string]string{
		"recipient_id":    "recipient1",
		"message":         "call me back",
		"idempotency_key": "key-1",
	}))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp pageEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Page.SenderID != "sender1" || resp.Page.RecipientID != "recipient1" || resp.Page.State != model.PageCreated {
		t.Errorf("unexpected page: %+v", resp.Page)
	}
	if len(q.Tasks) != 1 {
		t.Fatalf("expected 1 enqueued task, got %d", len(q.Tasks))
	}
	if q.Tasks[0].Task.PageID != resp.Page.ID || q.Tasks[0].Task.Attempt != 0 || q.Tasks[0].Delay != 0 {
		t.Errorf("unexpected enqueued task: %+v", q.Tasks[0])
	}
}

func TestHandleCreatePage_IdempotentReplayReturns200SamePage(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	seedActivePairing(t, st, "sender1", "recipient1")
	t.Setenv("DEV_USER_ID", "sender1")

	body := func() *bytes.Buffer {
		return jsonBody(map[string]string{
			"recipient_id":    "recipient1",
			"message":         "call me back",
			"idempotency_key": "dup-key",
		})
	}

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/pages", body()))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var first pageEnvelope
	if err := json.Unmarshal(rec1.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/pages", body()))
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var second pageEnvelope
	if err := json.Unmarshal(rec2.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.Page.ID != first.Page.ID {
		t.Errorf("replay returned a different page: %s vs %s", second.Page.ID, first.Page.ID)
	}
	if len(q.Tasks) != 1 {
		t.Errorf("replay must not enqueue a second time, got %d tasks", len(q.Tasks))
	}
}

func TestHandleCreatePage_BlockedPairingForbidden(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	seedActivePairing(t, st, "sender1", "recipient1")
	if err := st.BlockPairing(context.Background(), "sender1", "recipient1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEV_USER_ID", "sender1")

	req := httptest.NewRequest(http.MethodPost, "/pages", jsonBody(map[string]string{
		"recipient_id":    "recipient1",
		"message":         "hi",
		"idempotency_key": "key-2",
	}))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked pairing, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreatePage_NoPairingForbidden(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	if err := st.PutUser(context.Background(), model.User{ID: "sender1", DisplayName: "S", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEV_USER_ID", "sender1")

	req := httptest.NewRequest(http.MethodPost, "/pages", jsonBody(map[string]string{
		"recipient_id":    "stranger",
		"message":         "hi",
		"idempotency_key": "key-3",
	}))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no pairing, got %d: %s", rec.Code, rec.Body.String())
	}
}

func createTestPage(t *testing.T, srv *Server, idempotencyKey string) model.Page {
	t.Helper()
	t.Setenv("DEV_USER_ID", "sender1")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pages", jsonBody(map[string]string{
		"recipient_id":    "recipient1",
		"message":         "hi",
		"idempotency_key": idempotencyKey,
	})))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create page: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp pageEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Page
}

func TestHandleAckPage_SecondAckIsIdempotent(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	seedActivePairing(t, st, "sender1", "recipient1")
	page := createTestPage(t, srv, "ack-key-1")

	t.Setenv("DEV_USER_ID", "recipient1")

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, httptest.NewRequest(http.MethodPost, "/pages/"+page.ID+"/ack", jsonBody(map[string]string{"note": "omw"})))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first ack: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var firstAck pageEnvelope
	if err := json.Unmarshal(rec1.Body.Bytes(), &firstAck); err != nil {
		t.Fatal(err)
	}
	if firstAck.Page.State != model.PageAcked {
		t.Fatalf("expected acked state, got %s", firstAck.Page.State)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/pages/"+page.ID+"/ack", jsonBody(map[string]string{"note": "second try"})))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second ack: expected idempotent 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var secondAck pageEnvelope
	if err := json.Unmarshal(rec2.Body.Bytes(), &secondAck); err != nil {
		t.Fatal(err)
	}
	if secondAck.Page.AckNote != firstAck.Page.AckNote {
		t.Errorf("second ack must return the original ack, not overwrite it: %q vs %q", secondAck.Page.AckNote, firstAck.Page.AckNote)
	}
}

func TestHandleDeliveredReceipt_NeverRegressesAcked(t *testing.T) {
	st := fake.New()
	q := &fake.Enqueuer{}
	srv := New(st, q)
	seedActivePairing(t, st, "sender1", "recipient1")
	page := createTestPage(t, srv, "delivered-key-1")

	t.Setenv("DEV_USER_ID", "recipient1")
	ackRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(ackRec, httptest.NewRequest(http.MethodPost, "/pages/"+page.ID+"/ack", jsonBody(map[string]string{})))
	if ackRec.Code != http.StatusOK {
		t.Fatalf("ack: expected 200, got %d: %s", ackRec.Code, ackRec.Body.String())
	}

	deliveredRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(deliveredRec, httptest.NewRequest(http.MethodPost, "/pages/"+page.ID+"/delivered", nil))
	if deliveredRec.Code != http.StatusNoContent {
		t.Fatalf("delivered: expected 204, got %d: %s", deliveredRec.Code, deliveredRec.Body.String())
	}

	got, err := st.GetPage(context.Background(), page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.PageAcked {
		t.Errorf("delivered receipt regressed page state from acked to %s", got.State)
	}
}
