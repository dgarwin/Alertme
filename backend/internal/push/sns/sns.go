// Package sns implements push.Sender and push.Registrar against AWS SNS
// mobile push. SNS fronts APNs directly for iOS and FCM for Android; auth is
// the Lambda's IAM role — no OAuth, no service-account JSON at runtime.
package sns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/push"
)

// snsAPI is the subset of the SNS client Client depends on, so tests can
// substitute a fake.
type snsAPI interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
	DeleteEndpoint(ctx context.Context, params *sns.DeleteEndpointInput, optFns ...func(*sns.Options)) (*sns.DeleteEndpointOutput, error)
}

type Client struct {
	api snsAPI
}

var _ push.Sender = (*Client)(nil)

// New builds a Client from an SNS API client (e.g. sns.NewFromConfig(cfg)).
func New(api snsAPI) *Client {
	return &Client{api: api}
}

// apnsPayload mirrors the APNs payload the old fcm.go sent (aps dict with
// time-sensitive interruption level, custom sound, thread-id), with the page
// data keys as top-level siblings of aps rather than nested under FCM's
// "data" object.
type apnsPayload struct {
	Aps struct {
		InterruptionLevel string `json:"interruption-level"`
		Sound             string `json:"sound"`
		ThreadID          string `json:"thread-id"`
		MutableContent    int    `json:"mutable-content"`
	} `json:"aps"`
	PageID     string `json:"page_id"`
	Attempt    string `json:"attempt"`
	SenderName string `json:"sender_name"`
}

// gcmMessage is the SNS "fcmV1Message" envelope for the FCM HTTP v1 API.
type gcmMessage struct {
	FcmV1Message struct {
		Message struct {
			Data    map[string]string `json:"data"`
			Android struct {
				Priority string `json:"priority"`
			} `json:"android"`
		} `json:"message"`
	} `json:"fcmV1Message"`
}

func (c *Client) Send(ctx context.Context, device model.Device, n push.Notification) error {
	if device.EndpointARN == "" {
		return fmt.Errorf("device %s has no SNS endpoint ARN", device.Token)
	}

	input := &sns.PublishInput{
		TargetArn:        aws.String(device.EndpointARN),
		MessageStructure: aws.String("json"),
	}

	switch device.Platform {
	case "ios":
		var payload apnsPayload
		payload.Aps.InterruptionLevel = "time-sensitive"
		payload.Aps.Sound = "page.caf"
		payload.Aps.ThreadID = n.PageID
		payload.Aps.MutableContent = 1
		payload.PageID = n.PageID
		payload.Attempt = strconv.Itoa(n.Attempt)
		payload.SenderName = n.SenderName

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal apns payload: %w", err)
		}
		outer := map[string]string{"APNS": string(payloadJSON)}
		message, err := json.Marshal(outer)
		if err != nil {
			return fmt.Errorf("marshal sns message: %w", err)
		}
		input.Message = aws.String(string(message))
		input.MessageAttributes = map[string]types.MessageAttributeValue{
			"AWS.SNS.MOBILE.APNS.PUSH_TYPE": {DataType: aws.String("String"), StringValue: aws.String("alert")},
			"AWS.SNS.MOBILE.APNS.PRIORITY":  {DataType: aws.String("String"), StringValue: aws.String("10")},
		}

	case "android":
		var msg gcmMessage
		msg.FcmV1Message.Message.Data = map[string]string{
			"page_id":     n.PageID,
			"attempt":     strconv.Itoa(n.Attempt),
			"sender_name": n.SenderName,
		}
		msg.FcmV1Message.Message.Android.Priority = "high"

		payloadJSON, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal gcm payload: %w", err)
		}
		outer := map[string]string{"GCM": string(payloadJSON)}
		message, err := json.Marshal(outer)
		if err != nil {
			return fmt.Errorf("marshal sns message: %w", err)
		}
		input.Message = aws.String(string(message))

	default:
		return fmt.Errorf("unknown device platform %q", device.Platform)
	}

	_, err := c.api.Publish(ctx, input)
	if err == nil {
		return nil
	}

	var disabled *types.EndpointDisabledException
	if errors.As(err, &disabled) {
		if _, delErr := c.api.DeleteEndpoint(ctx, &sns.DeleteEndpointInput{EndpointArn: aws.String(device.EndpointARN)}); delErr != nil {
			log.Printf("delete disabled endpoint %s: %v", device.EndpointARN, delErr)
		}
		return push.ErrTokenGone
	}

	return fmt.Errorf("sns publish to %s: %w", device.EndpointARN, err)
}
