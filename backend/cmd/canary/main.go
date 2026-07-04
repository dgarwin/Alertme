// The canary lambda: every 5 minutes, bot A pages bot B through the real API
// and asserts the state machine advances; on success it pings the dead-man
// URL (HEARTBEAT_URL, e.g. healthchecks.io). A stalled scheduler therefore
// produces an email within minutes even while the API looks healthy.
//
// Bot A and bot B are two ordinary Cognito users, pre-provisioned out of
// band (and already paired with each other — the canary doesn't create the
// pairing, only exercises it). Configuration is entirely env-driven so the
// canary is a no-op — logged, not alarmed — until it's deliberately wired
// up: API_BASE_URL, COGNITO_CLIENT_ID, BOT_A_USERNAME, BOT_A_PASSWORD,
// BOT_B_USERNAME, BOT_B_PASSWORD, BOT_B_USER_ID (bot B's Cognito sub, used
// as recipient_id). HEARTBEAT_URL is optional; without it the run still
// verifies the state machine but has nothing to ping.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

type config struct {
	apiBaseURL   string
	clientID     string
	botAUsername string
	botAPassword string
	botBUsername string
	botBPassword string
	botBUserID   string
	heartbeatURL string
}

// loadConfig returns ok=false (and logs why) if any required variable is
// missing, so the disabled-by-default canary never alarms spuriously.
func loadConfig() (config, bool) {
	c := config{
		apiBaseURL:   os.Getenv("API_BASE_URL"),
		clientID:     os.Getenv("COGNITO_CLIENT_ID"),
		botAUsername: os.Getenv("BOT_A_USERNAME"),
		botAPassword: os.Getenv("BOT_A_PASSWORD"),
		botBUsername: os.Getenv("BOT_B_USERNAME"),
		botBPassword: os.Getenv("BOT_B_PASSWORD"),
		botBUserID:   os.Getenv("BOT_B_USER_ID"),
		heartbeatURL: os.Getenv("HEARTBEAT_URL"), // optional
	}
	required := map[string]string{
		"API_BASE_URL":      c.apiBaseURL,
		"COGNITO_CLIENT_ID": c.clientID,
		"BOT_A_USERNAME":    c.botAUsername,
		"BOT_A_PASSWORD":    c.botAPassword,
		"BOT_B_USERNAME":    c.botBUsername,
		"BOT_B_PASSWORD":    c.botBPassword,
		"BOT_B_USER_ID":     c.botBUserID,
	}
	for name, v := range required {
		if v == "" {
			log.Printf("canary disabled: missing %s", name)
			return config{}, false
		}
	}
	return c, true
}

type pageResponse struct {
	Page struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"page"`
}

func cognitoLogin(ctx context.Context, cip *cognitoidentityprovider.Client, clientID, username, password string) (string, error) {
	out, err := cip.InitiateAuth(ctx, &cognitoidentityprovider.InitiateAuthInput{
		AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		ClientId: &clientID,
		AuthParameters: map[string]string{
			"USERNAME": username,
			"PASSWORD": password,
		},
	})
	if err != nil {
		return "", fmt.Errorf("cognito login for %s: %w", username, err)
	}
	if out.AuthenticationResult == nil || out.AuthenticationResult.IdToken == nil {
		return "", fmt.Errorf("cognito login for %s: no id token in response (challenge required?)", username)
	}
	return *out.AuthenticationResult.IdToken, nil
}

func apiRequest(ctx context.Context, method, url, token string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func run(ctx context.Context) error {
	c, ok := loadConfig()
	if !ok {
		return nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	cip := cognitoidentityprovider.NewFromConfig(cfg)

	tokenA, err := cognitoLogin(ctx, cip, c.clientID, c.botAUsername, c.botAPassword)
	if err != nil {
		return err
	}
	tokenB, err := cognitoLogin(ctx, cip, c.clientID, c.botBUsername, c.botBPassword)
	if err != nil {
		return err
	}

	createBody := map[string]string{
		"recipient_id":    c.botBUserID,
		"message":         "canary ping",
		"idempotency_key": fmt.Sprintf("canary-%d", time.Now().UnixNano()),
	}
	resp, err := apiRequest(ctx, http.MethodPost, c.apiBaseURL+"/pages", tokenA, createBody)
	if err != nil {
		return fmt.Errorf("create canary page: %w", err)
	}
	var created pageResponse
	err = decodeAndClose(resp, &created)
	if err != nil {
		return fmt.Errorf("create canary page: %w", err)
	}
	if created.Page.ID == "" {
		return fmt.Errorf("create canary page: empty page id in response")
	}

	deadline := time.Now().Add(60 * time.Second)
	var last pageResponse
	for {
		resp, err := apiRequest(ctx, http.MethodGet, c.apiBaseURL+"/pages/"+created.Page.ID, tokenA, nil)
		if err != nil {
			return fmt.Errorf("poll canary page: %w", err)
		}
		if err := decodeAndClose(resp, &last); err != nil {
			return fmt.Errorf("poll canary page: %w", err)
		}
		if last.Page.State == "pushed" || last.Page.State == "delivered" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("canary page %s stuck in state %q after 60s", created.Page.ID, last.Page.State)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ackResp, err := apiRequest(ctx, http.MethodPost, c.apiBaseURL+"/pages/"+created.Page.ID+"/ack", tokenB, map[string]string{"note": "canary ack"})
	if err != nil {
		return fmt.Errorf("ack canary page: %w", err)
	}
	if err := drainAndClose(ackResp); err != nil {
		return fmt.Errorf("ack canary page: %w", err)
	}

	if c.heartbeatURL != "" {
		hbResp, err := http.Get(c.heartbeatURL)
		if err != nil {
			return fmt.Errorf("ping heartbeat: %w", err)
		}
		_ = drainAndClose(hbResp)
	}

	return nil
}

func decodeAndClose(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func drainAndClose(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func main() {
	lambda.Start(run)
}
