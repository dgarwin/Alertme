// The canary lambda: every 5 minutes, bot A pages bot B through the real API
// and asserts the state machine advances; on success it pings the dead-man
// URL (HEARTBEAT_URL, e.g. healthchecks.io). A stalled scheduler therefore
// produces an email within minutes even while the API looks healthy.
//
// build-out: authenticate the two bot users against Cognito (client
// credentials kept in SSM), POST /pages, poll GET /pages/{id} until state ∈
// {pushed, delivered} or 60s timeout, ack as bot B, then GET HEARTBEAT_URL.
package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	lambda.Start(func(ctx context.Context) error {
		panic("not implemented")
	})
}
