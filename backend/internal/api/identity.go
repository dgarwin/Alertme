package api

import (
	"context"
	"net/http"
	"os"

	"github.com/akrylysov/algnhsa"
)

type ctxKey int

const identityKey ctxKey = 0

// Identity is the caller as verified by the API Gateway JWT authorizer.
type Identity struct {
	UserID string // Cognito sub
	Email  string
}

// withIdentity extracts Cognito claims from the API Gateway v2 request
// context that algnhsa preserves on the request. Locally (no gateway in
// front of the handler, e.g. tests or `go run ./cmd/api`), DEV_USER_ID lets
// requests through as that fixed user; otherwise unauthenticated requests
// are rejected with 401.
func withIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req, ok := algnhsa.APIGatewayV2RequestFromContext(r.Context()); ok &&
			req.RequestContext.Authorizer != nil && req.RequestContext.Authorizer.JWT != nil {
			claims := req.RequestContext.Authorizer.JWT.Claims
			sub := claims["sub"]
			if sub != "" {
				ctx := context.WithValue(r.Context(), identityKey, Identity{UserID: sub, Email: claims["email"]})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		if devUser := os.Getenv("DEV_USER_ID"); devUser != "" {
			ctx := context.WithValue(r.Context(), identityKey, Identity{UserID: devUser, Email: os.Getenv("DEV_USER_EMAIL")})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// CallerFrom returns the verified identity placed by withIdentity.
func CallerFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey).(Identity)
	return id, ok
}
