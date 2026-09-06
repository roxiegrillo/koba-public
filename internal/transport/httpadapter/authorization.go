package httpadapter

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"

	internalauthorization "github.com/roxiegrillo/koba/v2/internal/authorization"
)

// AuthorizeOperation enforces authentication and all declared OAuth scopes.
func AuthorizeOperation(context *httprouter.Context, requiresAuthentication bool, requiredScopes []string) bool {
	err := internalauthorization.Authorize(context.Request.Context(), requiresAuthentication, requiredScopes)
	if err == nil {
		return true
	}
	if errors.Is(err, internalauthorization.ErrAuthenticationRequired) {
		WriteAuthenticationRequired(context)
		return false
	}

	var insufficient *internalauthorization.InsufficientScopeError
	if !errors.As(err, &insufficient) {
		WriteAuthenticationRequired(context)
		return false
	}
	context.Header(
		"WWW-Authenticate",
		fmt.Sprintf("Bearer error=\"insufficient_scope\", scope=%q", strings.Join(insufficient.Required, " ")),
	)
	context.JSON(http.StatusForbidden, httprouter.H{
		"status":  http.StatusForbidden,
		"error":   "forbidden",
		"message": insufficient.Error(),
	})
	return false
}

// WriteAuthenticationRequired emits the standard unauthorized response.
func WriteAuthenticationRequired(context *httprouter.Context) {
	context.Header("Cache-Control", "no-store")
	context.Header("WWW-Authenticate", "Bearer")
	context.JSON(http.StatusUnauthorized, httprouter.H{
		"status":  http.StatusUnauthorized,
		"error":   "unauthorized",
		"message": internalauthorization.ErrAuthenticationRequired.Error(),
	})
}
