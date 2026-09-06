// Package authorization evaluates transport-neutral subject and scope rules.
package authorization

import (
	"context"
	"errors"
	"strings"

	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

// ErrAuthenticationRequired marks a missing authenticated principal.
var ErrAuthenticationRequired = errors.New("authentication required")

// InsufficientScopeError reports the complete set of required OAuth scopes.
type InsufficientScopeError struct {
	Required []string
}

func (failure *InsufficientScopeError) Error() string {
	return "required scope is missing"
}

// Authorize evaluates authentication and exact, case-sensitive OAuth scopes.
func Authorize(ctx context.Context, requiresAuthentication bool, requiredScopes []string) error {
	if !requiresAuthentication {
		return nil
	}

	principal, authenticated := principal(ctx)
	if !authenticated {
		return ErrAuthenticationRequired
	}

	granted := make(map[string]struct{})
	for _, scope := range strings.Split(principal.Scope, " ") {
		if scope != "" {
			granted[scope] = struct{}{}
		}
	}
	for _, required := range requiredScopes {
		if _, exists := granted[required]; !exists {
			return &InsufficientScopeError{Required: append([]string(nil), requiredScopes...)}
		}
	}
	return nil
}

// Subject resolves the normalized subject established by identity middleware.
func Subject(ctx context.Context) (string, error) {
	principal, authenticated := principal(ctx)
	if !authenticated {
		return "", ErrAuthenticationRequired
	}
	return principal.UserID, nil
}

func principal(ctx context.Context) (appmiddleware.Principal, bool) {
	value, authenticated := appmiddleware.PrincipalFromContext(ctx)
	value.UserID = strings.TrimSpace(value.UserID)
	if !authenticated || value.UserID == "" {
		return appmiddleware.Principal{}, false
	}
	return value, true
}
