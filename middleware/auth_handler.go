package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/rs/zerolog/log"
)

type ResolverHandler struct {
	auth           *AuthMiddleware
	allowAnonymous bool
}

// Middleware returns an http.Handler that validates JWT tokens
// Use this to protect routes in your application
func (a *AuthMiddleware) Middleware() http.Handler {
	return &ResolverHandler{auth: a}
}

// OptionalMiddleware returns an http.Handler that authenticates a request when
// it carries an Authorization header. Requests without that header continue as
// anonymous requests; malformed or invalid credentials still fail closed.
func (a *AuthMiddleware) OptionalMiddleware() http.Handler {
	return &ResolverHandler{auth: a, allowAnonymous: true}
}

// ServeHTTP implements http.Handler interface
func (z *ResolverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.With().Str("component", "auth").Logger()
	if z.allowAnonymous && len(r.Header.Values("Authorization")) == 0 {
		return
	}

	token, err := jwtmiddleware.AuthHeaderTokenExtractor(r)
	if err != nil || token == "" {
		if err == nil {
			err = fmt.Errorf("missing authorization header")
		}
		logger.Warn().Err(err).Msg("JWT validation failed")
		writeUnauthorized(w)
		return
	}
	principal, claims, err := z.auth.validateClaims(r.Context(), token)
	if err != nil {
		logger.Warn().Err(err).Msg("JWT validation failed")
		writeUnauthorized(w)
		return
	}
	setAuthenticatedRequestContext(r, principal, claims)
}

func setAuthenticatedRequestContext(
	r *http.Request,
	principal Principal,
	claims *validator.ValidatedClaims,
) {
	ctx := WithPrincipal(r.Context(), principal)
	ctx = context.WithValue(ctx, jwtmiddleware.ContextKey{}, claims)
	// Deprecated compatibility values. New services must use
	// PrincipalFromContext instead of untyped string keys.
	ctx = context.WithValue(ctx, "user_id", principal.UserID)
	ctx = context.WithValue(ctx, "token_claims", claims)
	*r = *r.WithContext(ctx)
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  http.StatusUnauthorized,
		"error":   "unauthorized",
		"message": "authentication required",
	})
}
