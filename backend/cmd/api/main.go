// The api lambda: the whole REST API as a normal http.Handler, adapted to
// API Gateway HTTP API (payload v2) by algnhsa.
package main

import (
	"context"
	"log"
	"os"

	"github.com/akrylysov/algnhsa"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/dgarwin/alertme/backend/internal/api"
	"github.com/dgarwin/alertme/backend/internal/queue"
	"github.com/dgarwin/alertme/backend/internal/store/dynamo"
)

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}

	st := dynamo.New(dynamodb.NewFromConfig(cfg), os.Getenv("TABLE_NAME"), os.Getenv("GSI1_NAME"), os.Getenv("GSI2_NAME"))
	q := queue.NewSQS(sqs.NewFromConfig(cfg), os.Getenv("QUEUE_URL"))

	server := api.New(st, q)
	algnhsa.ListenAndServe(server.Router(), &algnhsa.Options{
		RequestType: algnhsa.RequestTypeAPIGatewayV2,
	})
}
