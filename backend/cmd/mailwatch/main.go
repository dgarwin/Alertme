// The mailwatch lambda: runs on an EventBridge schedule, checks Gmail
// (read-only OAuth) for unread mail from configured senders, and pages the
// recipient once per matching message via the normal page pipeline.
//
// Env:
//
//	TABLE_NAME, GSI1_NAME, GSI2_NAME, QUEUE_URL   (shared with other lambdas)
//	GMAIL_SECRET_ID     Secrets Manager id holding GmailCredential JSON
//	MAIL_SENDERS        comma-separated sender addresses to watch
//	RECIPIENT_USER_ID   Cognito sub of the user to page
//	BOT_USER_ID         synthetic sender id (default "bot-mailwatch")
//	BOT_NAME            name shown to the recipient (default "AlertMe Mail")
//	SEARCH_WINDOW       Gmail newer_than window (default "2d")
//
// Until the secret has a value (tools/gmail-oauth-setup.py), each run logs
// and exits successfully so the error alarm stays quiet.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/dgarwin/alertme/backend/internal/mailwatch"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store/dynamo"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	st := dynamo.New(dynamodb.NewFromConfig(cfg), os.Getenv("TABLE_NAME"), os.Getenv("GSI1_NAME"), os.Getenv("GSI2_NAME"))
	q := queue.NewSQS(sqs.NewFromConfig(cfg), os.Getenv("QUEUE_URL"))
	sm := secretsmanager.NewFromConfig(cfg)

	var senders []string
	for _, s := range strings.Split(os.Getenv("MAIL_SENDERS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			senders = append(senders, s)
		}
	}
	recipient := os.Getenv("RECIPIENT_USER_ID")
	secretID := os.Getenv("GMAIL_SECRET_ID")
	httpClient := &http.Client{Timeout: 10 * time.Second}

	lambda.Start(func(ctx context.Context) error {
		if len(senders) == 0 || recipient == "" || secretID == "" {
			return errors.New("MAIL_SENDERS, RECIPIENT_USER_ID and GMAIL_SECRET_ID are required")
		}
		cred, err := loadCredential(ctx, sm, secretID)
		if errors.Is(err, mailwatch.ErrNotConfigured) {
			log.Printf("gmail credential %q not set yet; run tools/gmail-oauth-setup.py. Skipping.", secretID)
			return nil
		}
		if err != nil {
			return err
		}
		w := &mailwatch.Watcher{
			Store: st, Queue: q,
			Mail:        &mailwatch.Gmail{HTTP: httpClient, Cred: cred, Window: envOr("SEARCH_WINDOW", "2d")},
			Senders:     senders,
			RecipientID: recipient,
			BotID:       envOr("BOT_USER_ID", "bot-mailwatch"),
			BotName:     envOr("BOT_NAME", "AlertMe Mail"),
			Now:         time.Now,
		}
		n, err := w.Run(ctx)
		if err != nil {
			return err
		}
		log.Printf("mailwatch: %d new page(s)", n)
		return nil
	})
}

func loadCredential(ctx context.Context, sm *secretsmanager.Client, id string) (mailwatch.GmailCredential, error) {
	var cred mailwatch.GmailCredential
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(id)})
	if err != nil {
		var nf *smtypes.ResourceNotFoundException
		if errors.As(err, &nf) {
			return cred, mailwatch.ErrNotConfigured
		}
		return cred, fmt.Errorf("get secret %s: %w", id, err)
	}
	if err := json.Unmarshal([]byte(aws.ToString(out.SecretString)), &cred); err != nil {
		return cred, fmt.Errorf("secret %s is not valid credential JSON", id)
	}
	if cred.RefreshToken == "" || cred.ClientID == "" {
		return cred, mailwatch.ErrNotConfigured
	}
	return cred, nil
}
