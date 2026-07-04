package api

import (
	"context"
	"net/http"

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
// context that algnhsa preserves on the request.
//
// IMPLEMENTATION NOTES: algnhsa.APIGatewayV2RequestFromContext →
// req.RequestContext.Authorizer.JWT.Claims["sub"] / ["email"]. Reject with
// 401 if absent (only happens when running locally without the gateway; a
// DEV_USER_ID env fallback keeps local runs usable).
func withIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := algnhsa.APIGatewayV2RequestFromContext(r.Context())
		_ = ok // TODO(build-out): extract claims per the note above
		http.Error(w, "identity middleware not implemented", http.StatusNotImplemented)
	})
}

// CallerFrom returns the verified identity placed by withIdentity.
func CallerFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey).(Identity)
	return id, ok
}
