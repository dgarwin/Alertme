// Package push abstracts sending a page alert to a device. SNS mobile push is
// the only implementation (it fronts both APNs and FCM/Android delivery).
package push

import (
	"context"
	"errors"

	"github.com/dgarwin/alertme/backend/internal/model"
)

// ErrTokenGone is returned when the provider reports the registration
// token/endpoint is no longer valid (SNS EndpointDisabled); the caller prunes
// the device.
var ErrTokenGone = errors.New("push token no longer valid")

// ErrPushNotConfigured is returned by Registrar.Register when the platform
// application ARN for the requested platform is unset (the SNS platform
// application hasn't been created out of band yet). Callers should store the
// device without an endpoint ARN and log a warning rather than fail the
// request — registration must succeed so the stack works before platform
// apps exist.
var ErrPushNotConfigured = errors.New("push not configured for platform")

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

// Registrar creates a push endpoint for a freshly-registered device token and
// returns its provider-side ARN. platform is "ios" or "android".
type Registrar interface {
	Register(ctx context.Context, platform, token string) (endpointARN string, err error)
}
