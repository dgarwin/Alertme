// Package fcm implements push.Sender against the FCM HTTP v1 API.
//
// IMPLEMENTATION NOTES (for the build-out pass):
//   - Credentials: service-account JSON from SSM SecureString $FCM_SA_PARAM,
//     loaded once at cold start; use golang.org/x/oauth2/google.JWTConfigFromJSON
//     with scope https://www.googleapis.com/auth/firebase.messaging and its
//     TokenSource (it caches/refreshes).
//   - POST https://fcm.googleapis.com/v1/projects/<project_id>/messages:send
//   - message.token = device token; data = {page_id, attempt, sender_name}.
//     android: priority HIGH. apns headers: apns-push-type alert,
//     apns-priority 10; payload.aps: interruption-level "time-sensitive",
//     sound "page.caf", thread-id page_id, mutable-content 1.
//   - HTTP 404/410 or errorCode UNREGISTERED → return push.ErrTokenGone.
//   - Retry 5xx/429 once with jitter; other errors return as-is (the SQS
//     redrive handles attempt-level retries).
package fcm

import (
	"context"
	"net/http"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/push"
)

type Client struct {
	http      *http.Client // must carry the oauth2 token source
	projectID string
}

var _ push.Sender = (*Client)(nil)

// New builds a client from raw service-account JSON.
func New(ctx context.Context, serviceAccountJSON []byte) (*Client, error) {
	panic("not implemented")
}

func (c *Client) Send(ctx context.Context, device model.Device, n push.Notification) error {
	panic("not implemented")
}
