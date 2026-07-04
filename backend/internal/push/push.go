// Package push abstracts sending a page alert to a device. FCM is the only
// implementation (it fronts both APNs and Android delivery).
package push

import (
	"context"
	"errors"

	"github.com/dgarwin/alertme/backend/internal/model"
)

// ErrTokenGone is returned when the provider reports the registration token is
// no longer valid (FCM UNREGISTERED); the caller prunes the device.
var ErrTokenGone = errors.New("push token no longer valid")

// Notification is deliberately minimal: the payload carries only the page ID
// and attempt — clients fetch content over the authenticated API, so message
// text never transits FCM.
type Notification struct {
	PageID     string
	Attempt    int
	SenderName string // shown before content fetch completes
}

type Sender interface {
	Send(ctx context.Context, device model.Device, n Notification) error
}
