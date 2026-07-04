// Package fcm implements push.Sender against the FCM HTTP v1 API.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/push"
)

const messagingScope = "https://www.googleapis.com/auth/firebase.messaging"

type Client struct {
	http      *http.Client // carries the oauth2 token source (see New)
	projectID string
}

var _ push.Sender = (*Client)(nil)

// serviceAccount is the subset of the Firebase/GCP service-account JSON we
// need; JWTConfigFromJSON parses the rest, but project_id isn't part of its
// output so we pull it separately.
type serviceAccount struct {
	ProjectID string `json:"project_id"`
}

// New builds a client from raw service-account JSON.
func New(ctx context.Context, serviceAccountJSON []byte) (*Client, error) {
	var sa serviceAccount
	if err := json.Unmarshal(serviceAccountJSON, &sa); err != nil {
		return nil, fmt.Errorf("parse fcm service account: %w", err)
	}
	if sa.ProjectID == "" {
		return nil, fmt.Errorf("fcm service account json missing project_id")
	}

	cfg, err := google.JWTConfigFromJSON(serviceAccountJSON, messagingScope)
	if err != nil {
		return nil, fmt.Errorf("parse fcm jwt config: %w", err)
	}

	return &Client{
		http:      oauth2.NewClient(ctx, cfg.TokenSource(ctx)),
		projectID: sa.ProjectID,
	}, nil
}

// fcmMessage mirrors the FCM HTTP v1 "send" request body — only the fields
// this sender needs.
type fcmMessage struct {
	Message struct {
		Token   string            `json:"token"`
		Data    map[string]string `json:"data"`
		Android *androidConfig    `json:"android,omitempty"`
		APNS    *apnsConfig       `json:"apns,omitempty"`
	} `json:"message"`
}

type androidConfig struct {
	Priority string `json:"priority"`
}

type apnsConfig struct {
	Headers map[string]string `json:"headers"`
	Payload struct {
		Aps struct {
			InterruptionLevel string `json:"interruption-level"`
			Sound             string `json:"sound"`
			ThreadID          string `json:"thread-id"`
			MutableContent    int    `json:"mutable-content"`
		} `json:"aps"`
	} `json:"payload"`
}

type fcmErrorResponse struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type      string `json:"@type"`
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func (c *Client) Send(ctx context.Context, device model.Device, n push.Notification) error {
	var msg fcmMessage
	msg.Message.Token = device.Token
	msg.Message.Data = map[string]string{
		"page_id":     n.PageID,
		"attempt":     strconv.Itoa(n.Attempt),
		"sender_name": n.SenderName,
	}
	msg.Message.Android = &androidConfig{Priority: "HIGH"}
	msg.Message.APNS = &apnsConfig{
		Headers: map[string]string{
			"apns-push-type": "alert",
			"apns-priority":  "10",
		},
	}
	msg.Message.APNS.Payload.Aps.InterruptionLevel = "time-sensitive"
	msg.Message.APNS.Payload.Aps.Sound = "page.caf"
	msg.Message.APNS.Payload.Aps.ThreadID = n.PageID
	msg.Message.APNS.Payload.Aps.MutableContent = 1

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal fcm message: %w", err)
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", c.projectID)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Intn(250)) * time.Millisecond
			select {
			case <-time.After(200*time.Millisecond + jitter):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build fcm request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fcm send request: %w", err)
			continue
		}
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read fcm response: %w", readErr)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || isUnregistered(respBody) {
			return push.ErrTokenGone
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("fcm send: status %d: %s", resp.StatusCode, string(respBody))
			continue
		}

		return fmt.Errorf("fcm send: status %d: %s", resp.StatusCode, string(respBody))
	}
	return lastErr
}

func isUnregistered(body []byte) bool {
	var e fcmErrorResponse
	if err := json.Unmarshal(body, &e); err != nil {
		return false
	}
	if e.Error.Status == "UNREGISTERED" {
		return true
	}
	for _, d := range e.Error.Details {
		if d.ErrorCode == "UNREGISTERED" {
			return true
		}
	}
	return false
}
