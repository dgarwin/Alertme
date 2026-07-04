package sns

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/dgarwin/alertme/backend/internal/push"
)

// registrarAPI is the subset of the SNS client Registrar depends on.
type registrarAPI interface {
	CreatePlatformEndpoint(ctx context.Context, params *sns.CreatePlatformEndpointInput, optFns ...func(*sns.Options)) (*sns.CreatePlatformEndpointOutput, error)
}

// Registrar implements push.Registrar: it creates (or reuses) an SNS
// platform endpoint for a device token, picking the platform application ARN
// by platform.
type Registrar struct {
	api     registrarAPI
	apnsARN string
	fcmARN  string
}

var _ push.Registrar = (*Registrar)(nil)

// NewRegistrar builds a Registrar. Either ARN may be empty when that
// platform's SNS platform application hasn't been configured yet — Register
// returns push.ErrPushNotConfigured for that platform rather than failing.
func NewRegistrar(api registrarAPI, apnsPlatformAppARN, fcmPlatformAppARN string) *Registrar {
	return &Registrar{api: api, apnsARN: apnsPlatformAppARN, fcmARN: fcmPlatformAppARN}
}

// existingEndpointARN matches the ARN AWS embeds in the InvalidParameter
// error message returned when CreatePlatformEndpoint is called again for a
// token that already has an endpoint with different attributes — the
// documented idempotency dance for SNS platform endpoints.
var existingEndpointARN = regexp.MustCompile(`arn:aws:sns:[a-zA-Z0-9-]+:[0-9]+:endpoint/[a-zA-Z0-9/_-]+`)

func (r *Registrar) Register(ctx context.Context, platform, token string) (string, error) {
	var platformAppARN string
	switch platform {
	case "ios":
		platformAppARN = r.apnsARN
	case "android":
		platformAppARN = r.fcmARN
	default:
		return "", fmt.Errorf("unknown platform %q", platform)
	}
	if platformAppARN == "" {
		return "", push.ErrPushNotConfigured
	}

	out, err := r.api.CreatePlatformEndpoint(ctx, &sns.CreatePlatformEndpointInput{
		PlatformApplicationArn: aws.String(platformAppARN),
		Token:                  aws.String(token),
	})
	if err == nil {
		return aws.ToString(out.EndpointArn), nil
	}

	var invalidParam *types.InvalidParameterException
	if errors.As(err, &invalidParam) {
		msg := aws.ToString(invalidParam.Message)
		if strings.Contains(msg, "already exists with the same Token") {
			if arn := existingEndpointARN.FindString(msg); arn != "" {
				return arn, nil
			}
		}
	}

	return "", fmt.Errorf("create sns platform endpoint: %w", err)
}
