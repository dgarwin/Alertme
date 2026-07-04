package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/nag"
	"github.com/dgarwin/alertme/backend/internal/push"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store"
	"github.com/dgarwin/alertme/backend/internal/store/fake"
)

// fakeSender is a push.Sender test double: it records every Send call and
// can be told to fail (optionally with push.ErrTokenGone) per device token.
type fakeSender struct {
	mu       sync.Mutex
	sent     []push.Notification
	failWith map[string]error // token -> error to return instead of sending
}

func (f *fakeSender) Send(ctx context.Context, device model.Device, n push.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failWith[device.Token]; ok {
		return err
	}
	f.sent = append(f.sent, n)
	return nil
}

func recordToBody(t *testing.T, task queue.PageTask) events.SQSMessage {
	t.Helper()
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	return events.SQSMessage{MessageId: "test-msg", Body: string(b)}
}

func TestHandleRecord_AckedPageDropsNoPush(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p1"
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "test page",
		State: model.PageCreated, Attempt: -1, IdempotencyKey: "k",
		CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(29 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// Move it to acked directly through the frozen contract.
	if err := st.AckPage(ctx, pageID, "", time.Now()); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 1})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("acked page must not be pushed, got %d sends", len(sender.sent))
	}
	if len(q.Tasks) != 0 {
		t.Errorf("acked page must not be re-enqueued, got %d tasks", len(q.Tasks))
	}
}

func TestHandleRecord_ExpiredPageSetsExpiredState(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p2"
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "m",
		State: model.PageCreated, IdempotencyKey: "k2",
		CreatedAt: time.Now().Add(-40 * time.Minute), ExpiresAt: time.Now().Add(-10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 1})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}

	page, err := st.GetPage(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if page.State != model.PageExpired {
		t.Errorf("expected page to expire, got state %s", page.State)
	}
	if len(sender.sent) != 0 {
		t.Errorf("expired page must not be pushed, got %d sends", len(sender.sent))
	}
	if len(q.Tasks) != 0 {
		t.Errorf("expired page must not be re-enqueued, got %d tasks", len(q.Tasks))
	}

	events, err := st.ListEvents(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Type == "expired" {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'expired' event to be appended")
	}
}

func TestHandleRecord_DuplicateAttemptDropped(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p3"
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "m",
		State: model.PagePushed, Attempt: 3, IdempotencyKey: "k3",
		CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(29 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	// Task asks for attempt 2, but the page already recorded attempt 3 — a
	// stale SQS redelivery of an old message.
	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 2})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("duplicate attempt must not push, got %d sends", len(sender.sent))
	}
	if len(q.Tasks) != 0 {
		t.Errorf("duplicate attempt must not re-enqueue, got %d tasks", len(q.Tasks))
	}
}

func TestHandleRecord_TokenGonePrunesDevice(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p4"
	if err := st.PutUser(ctx, model.User{ID: "sender1", DisplayName: "Sender", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "m",
		State: model.PageCreated, Attempt: -1, IdempotencyKey: "k4",
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	dev := model.Device{UserID: "recipient1", Token: "dead-token", Platform: "ios", UpdatedAt: time.Now()}
	if err := st.PutDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{failWith: map[string]error{"dead-token": push.ErrTokenGone}}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 0})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}

	devices, err := st.ListDevices(ctx, "recipient1")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Errorf("expected token-gone device to be pruned, still have %d", len(devices))
	}

	page, err := st.GetPage(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if page.State != model.PagePushed || page.Attempt != 0 {
		t.Errorf("expected page pushed at attempt 0 despite dead device, got state=%s attempt=%d", page.State, page.Attempt)
	}
	if len(q.Tasks) != 1 {
		t.Fatalf("expected next attempt to be enqueued, got %d tasks", len(q.Tasks))
	}
}

func TestHandleRecord_ScheduleExhaustionEnqueuesFinalExpiryTick(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p5"

	// finalAttempt is the last attempt the schedule still has a delay for;
	// NextDelay(finalAttempt+1) is exhausted (see nag.NextDelay).
	finalAttempt := len(nag.Delays)
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "m",
		State: model.PageCreated, Attempt: -1, IdempotencyKey: "k5",
		CreatedAt: time.Now().Add(-25 * time.Minute), ExpiresAt: time.Now().Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// The page already recorded attempt finalAttempt-1 (its last push); the
	// incoming task is asking for the final scheduled attempt.
	if err := st.SetPageState(ctx, pageID, []model.PageState{model.PageCreated}, model.PagePushed, finalAttempt-1); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	// Confirm the schedule really is exhausted one past this attempt, so the
	// test exercises the "final tick" branch rather than a normal re-enqueue.
	if _, ok := nag.NextDelay(finalAttempt + 1); ok {
		t.Fatalf("test setup invalid: NextDelay(%d) should be exhausted", finalAttempt+1)
	}

	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: finalAttempt})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}

	last, ok := q.Last()
	if !ok {
		t.Fatal("expected a final expiry tick to be enqueued")
	}
	if last.Task.Attempt != finalAttempt+1 {
		t.Errorf("expected final tick attempt %d, got %d", finalAttempt+1, last.Task.Attempt)
	}
	if last.Delay <= 0 || last.Delay > maxFinalDelay {
		t.Errorf("final tick delay %s should be in (0, %s]", last.Delay, maxFinalDelay)
	}
}

func TestHandleRecord_MissingPageDropsWithoutError(t *testing.T) {
	st := fake.New()
	h := &handler{store: st, queue: &fake.Enqueuer{}, sender: &fakeSender{}}
	if err := h.handleRecord(context.Background(), recordToBody(t, queue.PageTask{PageID: "missing", Attempt: 0})); err != nil {
		t.Errorf("expected nil error for a not-found page, got %v", err)
	}
}

// sanity check that fake.Store surfaces store.ErrConflict the same way the
// real dynamo store would, since handleRecord relies on that to detect
// "already advanced past pushed" without treating it as an error.
func TestFakeStoreConflictOnRegressiveSetPageState(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	if err := st.CreatePage(ctx, model.Page{ID: "p6", SenderID: "s", RecipientID: "r", Message: "m", State: model.PageDelivered, IdempotencyKey: "k6", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	err := st.SetPageState(ctx, "p6", []model.PageState{model.PageCreated, model.PagePushed}, model.PagePushed, 1)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected store.ErrConflict, got %v", err)
	}
}

// Regression: a delivered (or seen) page keeps nagging, and each attempt must
// still advance the attempt counter in place — without it, the step-3 dedup
// guard can't suppress an SQS duplicate, which would fork a second nag chain.
func TestHandleRecord_DeliveredPageRecordsAttemptForDedup(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	pageID := "p7"
	if err := st.CreatePage(ctx, model.Page{
		ID: pageID, SenderID: "sender1", RecipientID: "recipient1", Message: "m",
		State: model.PageDelivered, Attempt: 1, IdempotencyKey: "k7",
		CreatedAt: time.Now().Add(-2 * time.Minute), ExpiresAt: time.Now().Add(28 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	q := &fake.Enqueuer{}
	h := &handler{store: st, queue: q, sender: sender}

	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 2})); err != nil {
		t.Fatalf("handleRecord returned error: %v", err)
	}

	page, err := st.GetPage(ctx, pageID)
	if err != nil {
		t.Fatal(err)
	}
	if page.State != model.PageDelivered {
		t.Errorf("state must not regress from delivered, got %s", page.State)
	}
	if page.Attempt != 2 {
		t.Errorf("attempt must advance to 2 on a delivered page, got %d", page.Attempt)
	}

	// The duplicate of attempt 2 must now be suppressed by the dedup guard.
	if err := h.handleRecord(ctx, recordToBody(t, queue.PageTask{PageID: pageID, Attempt: 2})); err != nil {
		t.Fatalf("duplicate handleRecord returned error: %v", err)
	}
	if got := len(q.Tasks); got != 1 {
		t.Errorf("duplicate attempt must not re-enqueue: want 1 queued task, got %d", got)
	}
}
