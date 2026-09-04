package middleware

import "context"

// UserInfo contains the authenticated user's information extracted from the JWT
type UserInfo struct {
	UserID      string   // The Auth0 user ID (sub claim)
	Scope       string   // The scopes granted
	Permissions []string // The permissions granted
}

// Principal is the authenticated identity exposed to Koba API methods.
// It aliases UserInfo to preserve the existing public API while giving new
// services transport-neutral terminology.
type Principal = UserInfo

type principalContextKey struct{}

// WithPrincipal stores an immutable copy of the authenticated principal.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	principal.Permissions = append([]string(nil), principal.Permissions...)
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns a copy of the authenticated principal.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok {
		return Principal{}, false
	}
	principal.Permissions = append([]string(nil), principal.Permissions...)
	return principal, true
}
