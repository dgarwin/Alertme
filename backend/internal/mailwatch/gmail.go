package mailwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GmailCredential is the JSON stored in Secrets Manager by
// tools/gmail-oauth-setup.py. Scope is gmail.readonly.
type GmailCredential struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

// Gmail reads the mailbox via the Gmail REST API using an OAuth refresh
// token. It never modifies mail (read-only scope).
type Gmail struct {
	HTTP     *http.Client
	Cred     GmailCredential
	TokenURL string // default https://oauth2.googleapis.com/token
	APIBase  string // default https://gmail.googleapis.com/gmail/v1
	// Window limits the search, e.g. "2d". Older unread mail is ignored.
	Window string
}

func (g *Gmail) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}

func (g *Gmail) accessToken(ctx context.Context) (string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {g.Cred.ClientID},
		"client_secret": {g.Cred.ClientSecret},
		"refresh_token": {g.Cred.RefreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail token refresh: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		// invalid_grant = refresh token revoked/expired: re-run the setup script.
		return "", fmt.Errorf("gmail token refresh: HTTP %d %s %s", resp.StatusCode, body.Error, body.Description)
	}
	return body.AccessToken, nil
}

func (g *Gmail) get(ctx context.Context, token, path string, q url.Values, out any) error {
	base := g.APIBase
	if base == "" {
		base = "https://gmail.googleapis.com/gmail/v1"
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client().Do(req)
	if err != nil {
		return fmt.Errorf("gmail GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gmail GET %s: HTTP %d: %s", path, resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Query builds the Gmail search for unread mail from any sender.
func Query(senders []string, window string) string {
	parts := make([]string, len(senders))
	for i, s := range senders {
		parts[i] = "from:" + strings.TrimSpace(s)
	}
	q := "is:unread {" + strings.Join(parts, " ") + "}"
	if window != "" {
		q += " newer_than:" + window
	}
	return q
}

func (g *Gmail) UnreadFrom(ctx context.Context, senders []string) ([]Message, error) {
	if g.Cred.RefreshToken == "" {
		return nil, ErrNotConfigured
	}
	token, err := g.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	q := url.Values{"q": {Query(senders, g.Window)}, "maxResults": {"25"}}
	if err := g.get(ctx, token, "/users/me/messages", q, &list); err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(list.Messages))
	for _, m := range list.Messages {
		var full struct {
			ID      string `json:"id"`
			Payload struct {
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"payload"`
		}
		mq := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "Subject"}}
		if err := g.get(ctx, token, "/users/me/messages/"+url.PathEscape(m.ID), mq, &full); err != nil {
			return nil, err
		}
		msg := Message{ID: m.ID}
		for _, h := range full.Payload.Headers {
			switch strings.ToLower(h.Name) {
			case "from":
				msg.From = h.Value
			case "subject":
				msg.Subject = h.Value
			}
		}
		out = append(out, msg)
	}
	return out, nil
}
