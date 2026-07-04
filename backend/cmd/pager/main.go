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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/nag"
	"github.com/dgarwin/alertme/backend/internal/push"
	"github.com/dgarwin/alertme/backend/internal/push/fcm"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store"
	"github.com/dgarwin/alertme/backend/internal/store/dynamo"
)

// maxFinalDelay caps the last nag tick enqueued once the schedule is
// exhausted but the page hasn't expired yet (SQS DelaySeconds max is 900s).
const maxFinalDelay = 15 * time.Minute

type handler struct {
	store store.Store
	queue queue.Enqueuer

	// FCM credentials come from SSM and are fetched once per cold start.
	ssmClient *ssm.Client
	fcmParam  string

	senderOnce sync.Once
	sender     push.Sender // pre-set (e.g. by tests) to skip the SSM/FCM lazy init entirely
	senderErr  error
}

func (h *handler) getSender(ctx context.Context) (push.Sender, error) {
	if h.sender != nil {
		return h.sender, nil
	}
	h.senderOnce.Do(func() {
		out, err := h.ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
			Name:           aws.String(h.fcmParam),
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			h.senderErr = fmt.Errorf("load fcm service account from %s: %w", h.fcmParam, err)
			return
		}
		client, err := fcm.New(ctx, []byte(aws.ToString(out.Parameter.Value)))
		if err != nil {
			h.senderErr = fmt.Errorf("init fcm client: %w", err)
			return
		}
		h.sender = client
	})
	return h.sender, h.senderErr
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
	if err := json.Unmarshal([]byte(record.Body), &task); err != nil {
		// A malformed body can never succeed on redrive either; log and
		// drop rather than poisoning the queue forever.
		log.Printf("record %s: unmarshal page task: %v", record.MessageId, err)
		return nil
	}

	page, err := h.store.GetPage(ctx, task.PageID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			log.Printf("page %s not found, dropping attempt %d", task.PageID, task.Attempt)
			return nil
		}
		return fmt.Errorf("load page %s: %w", task.PageID, err)
	}

	if !page.State.Active() {
		log.Printf("page %s state=%s inactive, dropping attempt %d", page.ID, page.State, task.Attempt)
		return nil
	}

	activeStates := []model.PageState{model.PageCreated, model.PagePushed, model.PageDelivered, model.PageSeen}

	now := time.Now()
	if now.After(page.ExpiresAt) {
		if err := h.store.SetPageState(ctx, page.ID, activeStates, model.PageExpired, page.Attempt); err != nil && !errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("expire page %s: %w", page.ID, err)
		}
		if err := h.store.AppendEvent(ctx, model.PageEvent{PageID: page.ID, Type: "expired", At: now}); err != nil {
			log.Printf("page %s: failed to append expired event: %v", page.ID, err)
		}
		return nil
	}

	if page.Attempt >= task.Attempt {
		log.Printf("page %s: attempt %d already ran (current %d), dropping duplicate", page.ID, task.Attempt, page.Attempt)
		return nil
	}

	devices, err := h.store.ListDevices(ctx, page.RecipientID)
	if err != nil {
		return fmt.Errorf("list devices for %s: %w", page.RecipientID, err)
	}

	if len(devices) == 0 {
		if err := h.store.AppendEvent(ctx, model.PageEvent{PageID: page.ID, Type: "no_devices", At: now}); err != nil {
			log.Printf("page %s: failed to append no_devices event: %v", page.ID, err)
		}
	} else {
		senderName := page.SenderID
		if u, err := h.store.GetUser(ctx, page.SenderID); err == nil {
			senderName = u.DisplayName
		}

		sender, err := h.getSender(ctx)
		if err != nil {
			return fmt.Errorf("push sender: %w", err)
		}

		notification := push.Notification{PageID: page.ID, Attempt: task.Attempt, SenderName: senderName}
		for _, d := range devices {
			if err := sender.Send(ctx, d, notification); err != nil {
				if errors.Is(err, push.ErrTokenGone) {
					if deleter, ok := h.store.(store.DeviceDeleter); ok {
						if delErr := deleter.DeleteDevice(ctx, d.UserID, d.Token); delErr != nil {
							log.Printf("page %s: failed to prune dead device %s: %v", page.ID, d.Token, delErr)
						}
					}
					continue
				}
				log.Printf("page %s: push to device %s failed: %v", page.ID, d.Token, err)
			}
		}
	}

	// Record the attempt and move created→pushed. Delivered/seen pages keep
	// nagging (delivered ≠ acknowledged) but must not regress state, so the
	// attempt counter is bumped in-place for them — the step-3 dedup guard
	// depends on it advancing every attempt, or an SQS duplicate would fork a
	// second nag chain. Losing every condition means the page reached a
	// terminal state between step 1 and here; nothing to record.
	if err := h.recordAttempt(ctx, page.ID, task.Attempt); err != nil {
		return err
	}
	if err := h.store.AppendEvent(ctx, model.PageEvent{PageID: page.ID, Type: "push_attempt", At: now}); err != nil {
		log.Printf("page %s: failed to append push_attempt event: %v", page.ID, err)
	}

	nextAttempt := task.Attempt + 1
	if delay, ok := nag.NextDelay(nextAttempt); ok {
		if err := h.queue.Enqueue(ctx, queue.PageTask{PageID: page.ID, Attempt: nextAttempt}, delay); err != nil {
			return fmt.Errorf("enqueue attempt %d for page %s: %w", nextAttempt, page.ID, err)
		}
		return nil
	}

	// Schedule exhausted: if the page hasn't hit ExpiresAt yet, enqueue one
	// last tick right at (or just before) the expiry boundary so step 2
	// eventually marks it expired instead of nagging silently forever.
	if now.Before(page.ExpiresAt) {
		remaining := page.ExpiresAt.Sub(now)
		if remaining > maxFinalDelay {
			remaining = maxFinalDelay
		}
		if err := h.queue.Enqueue(ctx, queue.PageTask{PageID: page.ID, Attempt: nextAttempt}, remaining); err != nil {
			return fmt.Errorf("enqueue final expiry tick for page %s: %w", page.ID, err)
		}
	}
	return nil
}

// recordAttempt bumps the attempt counter without ever regressing state:
// created/pushed→pushed, else delivered→delivered, else seen→seen.
func (h *handler) recordAttempt(ctx context.Context, pageID string, attempt int) error {
	transitions := []struct {
		from []model.PageState
		to   model.PageState
	}{
		{[]model.PageState{model.PageCreated, model.PagePushed}, model.PagePushed},
		{[]model.PageState{model.PageDelivered}, model.PageDelivered},
		{[]model.PageState{model.PageSeen}, model.PageSeen},
	}
	for _, t := range transitions {
		err := h.store.SetPageState(ctx, pageID, t.from, t.to, attempt)
		if err == nil {
			return nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("record attempt %d on page %s: %w", attempt, pageID, err)
		}
	}
	return nil // page reached a terminal state concurrently
}

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}

	h := &handler{
		store:     dynamo.New(dynamodb.NewFromConfig(cfg), os.Getenv("TABLE_NAME"), os.Getenv("GSI1_NAME"), os.Getenv("GSI2_NAME")),
		queue:     queue.NewSQS(sqs.NewFromConfig(cfg), os.Getenv("QUEUE_URL")),
		ssmClient: ssm.NewFromConfig(cfg),
		fcmParam:  os.Getenv("FCM_SA_PARAM"),
	}
	lambda.Start(h.handleBatch)
}
