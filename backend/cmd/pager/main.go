// The pager lambda: consumes PageTask messages from the SQS nag queue and
// drives the page state machine. This is the heart of the product.
//
// PER-MESSAGE ALGORITHM (build-out implements handleTask):
//  1. Load the page. Not found → drop (log). State not Active() → drop
//     silently: this is how acks/cancels "cancel" future nags.
//  2. Past ExpiresAt → SetPageState(from active states → expired) + append
//     "expired" event → drop.
//  3. Idempotency guard: if page.Attempt >= task.Attempt the attempt already
//     ran (SQS at-least-once duplicate) → drop.
//  4. Load recipient devices; none → append event "no_devices" and fall
//     through to re-enqueue (a device may register mid-page).
//  5. Send push.Notification to EVERY device. push.ErrTokenGone → prune that
//     device. Any success counts; total failure is retried by the next tick.
//  6. SetPageState(created→pushed, attempt=task.Attempt) — never regresses
//     delivered/seen — and append "push_attempt" event.
//  7. nag.NextDelay(task.Attempt+1): ok → Enqueue next PageTask; exhausted →
//     nothing (step 2 expires the page if a final tick arrives after expiry,
//     so enqueue one last tick at the expiry boundary when the schedule ends
//     before ExpiresAt).
//
// Returning an error from a batch item sends it to redrive (and eventually
// the DLQ alarm), so ONLY return errors for infrastructure failures — never
// for terminal page states.
package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/dgarwin/alertme/backend/internal/queue"
)

type handler struct {
	// build-out: store.Store, push.Sender (fcm), queue.Enqueuer, nag schedule
}

func (h *handler) handleBatch(ctx context.Context, ev events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse
	for _, record := range ev.Records {
		if err := h.handleRecord(ctx, record); err != nil {
			log.Printf("record %s failed: %v", record.MessageId, err)
			resp.BatchItemFailures = append(resp.BatchItemFailures,
				events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return resp, nil
}

func (h *handler) handleRecord(ctx context.Context, record events.SQSMessage) error {
	var task queue.PageTask
	_ = task // build-out: unmarshal record.Body, then run the algorithm above
	panic("not implemented")
}

func main() {
	h := &handler{} // build-out: wire aws config, dynamo store, fcm, sqs
	lambda.Start(h.handleBatch)
}
