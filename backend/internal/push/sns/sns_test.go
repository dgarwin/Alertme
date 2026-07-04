package sns

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/dgarwin/alertme/backend/internal/model"
	"github.com/dgarwin/alertme/backend/internal/push"
)

// fakeSNS is a snsAPI test double.
type fakeSNS struct {
	publishInput *sns.PublishInput
	publishCalls int
	publishErr   error

	deleteEndpointARN string
	deleteCalls       int
	deleteErr         error
}

func (f *fakeSNS) Publish(_ context.Context, params *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.publishCalls++
	f.publishInput = params
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	return &sns.PublishOutput{MessageId: aws.String("msg-1")}, nil
}

func (f *fakeSNS) DeleteEndpoint(_ context.Context, params *sns.DeleteEndpointInput, _ ...func(*sns.Options)) (*sns.DeleteEndpointOutput, error) {
	f.deleteCalls++
	f.deleteEndpointARN = aws.ToString(params.EndpointArn)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &sns.DeleteEndpointOutput{}, nil
}

func TestSend_IOSBuildsAPNSMessageAndAttributes(t *testing.T) {
	fake := &fakeSNS{}
	c := New(fake)
	dev := model.Device{UserID: "u1", Token: "tok", Platform: "ios", EndpointARN: "arn:aws:sns:us-east-1:123:endpoint/APNS/app/abc"}
	n := push.Notification{PageID: "p1", Attempt: 2, SenderName: "Alice"}

	if err := c.Send(context.Background(), dev, n); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if fake.publishCalls != 1 {
		t.Fatalf("expected 1 Publish call, got %d", fake.publishCalls)
	}
	if got := aws.ToString(fake.publishInput.TargetArn); got != dev.EndpointARN {
		t.Errorf("TargetArn = %q, want %q", got, dev.EndpointARN)
	}
	if got := aws.ToString(fake.publishInput.MessageStructure); got != "json" {
		t.Errorf("MessageStructure = %q, want json", got)
	}

	var outer map[string]string
	if err := json.Unmarshal([]byte(aws.ToString(fake.publishInput.Message)), &outer); err != nil {
		t.Fatalf("outer message not valid json: %v", err)
	}
	apnsJSON, ok := outer["APNS"]
	if !ok {
		t.Fatalf("expected APNS key in outer message, got %v", outer)
	}
	var payload apnsPayload
	if err := json.Unmarshal([]byte(apnsJSON), &payload); err != nil {
		t.Fatalf("APNS value not valid json: %v", err)
	}
	if payload.Aps.InterruptionLevel != "time-sensitive" || payload.Aps.Sound != "page.caf" ||
		payload.Aps.ThreadID != "p1" || payload.Aps.MutableContent != 1 {
		t.Errorf("unexpected aps dict: %+v", payload.Aps)
	}
	if payload.PageID != "p1" || payload.Attempt != "2" || payload.SenderName != "Alice" {
		t.Errorf("unexpected top-level data keys: %+v", payload)
	}

	attrs := fake.publishInput.MessageAttributes
	pushType, ok := attrs["AWS.SNS.MOBILE.APNS.PUSH_TYPE"]
	if !ok || aws.ToString(pushType.StringValue) != "alert" {
		t.Errorf("expected PUSH_TYPE=alert message attribute, got %+v", attrs)
	}
	priority, ok := attrs["AWS.SNS.MOBILE.APNS.PRIORITY"]
	if !ok || aws.ToString(priority.StringValue) != "10" {
		t.Errorf("expected PRIORITY=10 message attribute, got %+v", attrs)
	}
}

func TestSend_AndroidBuildsGCMMessage(t *testing.T) {
	fake := &fakeSNS{}
	c := New(fake)
	dev := model.Device{UserID: "u1", Token: "tok", Platform: "android", EndpointARN: "arn:aws:sns:us-east-1:123:endpoint/GCM/app/abc"}
	n := push.Notification{PageID: "p2", Attempt: 0, SenderName: "Bob"}

	if err := c.Send(context.Background(), dev, n); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var outer map[string]string
	if err := json.Unmarshal([]byte(aws.ToString(fake.publishInput.Message)), &outer); err != nil {
		t.Fatalf("outer message not valid json: %v", err)
	}
	gcmJSON, ok := outer["GCM"]
	if !ok {
		t.Fatalf("expected GCM key in outer message, got %v", outer)
	}
	var msg gcmMessage
	if err := json.Unmarshal([]byte(gcmJSON), &msg); err != nil {
		t.Fatalf("GCM value not valid json: %v", err)
	}
	data := msg.FcmV1Message.Message.Data
	if data["page_id"] != "p2" || data["attempt"] != "0" || data["sender_name"] != "Bob" {
		t.Errorf("unexpected data keys: %+v", data)
	}
	if msg.FcmV1Message.Message.Android.Priority != "high" {
		t.Errorf("expected android priority high, got %q", msg.FcmV1Message.Message.Android.Priority)
	}
}

func TestSend_EmptyEndpointARNErrorsWithoutPublishing(t *testing.T) {
	fake := &fakeSNS{}
	c := New(fake)
	dev := model.Device{UserID: "u1", Token: "tok", Platform: "ios"}

	err := c.Send(context.Background(), dev, push.Notification{PageID: "p3"})
	if err == nil {
		t.Fatal("expected an error for a device with no endpoint ARN")
	}
	if errors.Is(err, push.ErrTokenGone) {
		t.Error("missing endpoint ARN must not be reported as ErrTokenGone (pruning would be wrong)")
	}
	if fake.publishCalls != 0 {
		t.Errorf("expected Publish not to be called, got %d calls", fake.publishCalls)
	}
}

func TestSend_EndpointDisabledDeletesEndpointAndReturnsErrTokenGone(t *testing.T) {
	fake := &fakeSNS{publishErr: &types.EndpointDisabledException{Message: aws.String("Endpoint is disabled")}}
	c := New(fake)
	dev := model.Device{UserID: "u1", Token: "tok", Platform: "ios", EndpointARN: "arn:aws:sns:us-east-1:123:endpoint/APNS/app/dead"}

	err := c.Send(context.Background(), dev, push.Notification{PageID: "p4"})
	if !errors.Is(err, push.ErrTokenGone) {
		t.Fatalf("expected push.ErrTokenGone, got %v", err)
	}
	if fake.deleteCalls != 1 {
		t.Fatalf("expected DeleteEndpoint to be called once, got %d", fake.deleteCalls)
	}
	if fake.deleteEndpointARN != dev.EndpointARN {
		t.Errorf("DeleteEndpoint called with %q, want %q", fake.deleteEndpointARN, dev.EndpointARN)
	}
}
