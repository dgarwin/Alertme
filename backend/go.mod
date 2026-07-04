module github.com/dgarwin/alertme/backend

go 1.24

require (
	github.com/akrylysov/algnhsa v1.1.0
	github.com/aws/aws-lambda-go v1.47.0
	github.com/aws/aws-sdk-go-v2 v1.42.1
	github.com/aws/aws-sdk-go-v2/config v1.28.6
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.20.50
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.63.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.59.2
	github.com/aws/aws-sdk-go-v2/service/sqs v1.37.2
	github.com/aws/aws-sdk-go-v2/service/ssm v1.69.5
	github.com/go-chi/chi/v5 v5.1.0
	golang.org/x/oauth2 v0.24.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.47 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.34.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.24.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.28.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.2 // indirect
	github.com/aws/smithy-go v1.27.3 // indirect
)
