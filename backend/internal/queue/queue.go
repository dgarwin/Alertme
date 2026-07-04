// Package queue wraps the SQS nag queue. One message type flows through it.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// PageTask asks the pager to make push attempt N for a page. The pager decides
// at fire time whether the page still needs it (acked/expired ⇒ drop).
type PageTask struct {
	PageID  string `json:"page_id"`
	Attempt int    `json:"attempt"`
}

type Enqueuer interface {
	Enqueue(ctx context.Context, task PageTask, delay time.Duration) error
}

type SQS struct {
	client   *sqs.Client
	queueURL string
}

var _ Enqueuer = (*SQS)(nil)

func NewSQS(client *sqs.Client, queueURL string) *SQS {
	return &SQS{client: client, queueURL: queueURL}
}

func (q *SQS) Enqueue(ctx context.Context, task PageTask, delay time.Duration) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal page task: %w", err)
	}
	_, err = q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:     aws.String(q.queueURL),
		MessageBody:  aws.String(string(body)),
		DelaySeconds: int32(delay / time.Second), // schedule guarantees ≤900
	})
	if err != nil {
		return fmt.Errorf("enqueue page task %s attempt %d: %w", task.PageID, task.Attempt, err)
	}
	return nil
}
