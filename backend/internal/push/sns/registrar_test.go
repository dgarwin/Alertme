package sns

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/dgarwin/alertme/backend/internal/push"
)

// fakeRegistrarSNS is a registrarAPI test double.
type fakeRegistrarSNS struct {
	calls       int
	lastInput   *sns.CreatePlatformEndpointInput
	err         error
	endpointArn string
}

func (f *fakeRegistrarSNS) CreatePlatformEndpoint(_ context.Context, params *sns.CreatePlatformEndpointInput, _ ...func(*sns.Options)) (*sns.CreatePlatformEndpointOutput, error) {
	f.calls++
	f.lastInput = params
	if f.err != nil {
		return nil, f.err
	}
	return &sns.CreatePlatformEndpointOutput{EndpointArn: aws.String(f.endpointArn)}, nil
}

func TestRegistrar_HappyPath(t *testing.T) {
	fake := &fakeRegistrarSNS{endpointArn: "arn:aws:sns:us-east-1:123:endpoint/APNS/app/new"}
	r := NewRegistrar(fake, "arn:aws:sns:us-east-1:123:app/APNS/app", "")

	arn, err := r.Register(context.Background(), "ios", "device-token")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if arn != fake.endpointArn {
		t.Errorf("arn = %q, want %q", arn, fake.endpointArn)
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 CreatePlatformEndpoint call, got %d", fake.calls)
	}
	if got := aws.ToString(fake.lastInput.Token); got != "device-token" {
		t.Errorf("Token = %q, want device-token", got)
	}
}

func TestRegistrar_NotConfiguredPlatform(t *testing.T) {
	fake := &fakeRegistrarSNS{}
	r := NewRegistrar(fake, "", "") // neither platform app configured

	_, err := r.Register(context.Background(), "android", "device-token")
	if !errors.Is(err, push.ErrPushNotConfigured) {
		t.Fatalf("expected push.ErrPushNotConfigured, got %v", err)
	}
	if fake.calls != 0 {
		t.Errorf("expected CreatePlatformEndpoint not to be called, got %d calls", fake.calls)
	}
}

func TestRegistrar_IdempotentExistingEndpointParsed(t *testing.T) {
	existingARN := "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/app/existing-endpoint-id"
	fake := &fakeRegistrarSNS{err: &types.InvalidParameterException{
		Message: aws.String("Invalid parameter: Token Reason: Endpoint " + existingARN + " already exists with the same Token, but different attributes."),
	}}
	r := NewRegistrar(fake, "", "arn:aws:sns:us-east-1:123:app/GCM/app")

	arn, err := r.Register(context.Background(), "android", "device-token")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if arn != existingARN {
		t.Errorf("arn = %q, want %q", arn, existingARN)
	}
}
