package koba

import (
	"fmt"
	"net/http"
	"strings"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

func authorizeOperation(c *httprouter.Context, requiredScopes []string) bool {
	if len(requiredScopes) == 0 {
		return true
	}

	principal, authenticated := appmiddleware.PrincipalFromContext(c.Request.Context())
	if !authenticated {
		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, httprouter.H{
			"status":  http.StatusUnauthorized,
			"error":   "unauthorized",
			"message": "authentication required",
		})
		return false
	}

	granted := make(map[string]struct{})
	for _, scope := range strings.Split(principal.Scope, " ") {
		if scope != "" {
			granted[scope] = struct{}{}
		}
	}
	for _, required := range requiredScopes {
		if _, exists := granted[required]; !exists {
			c.Header(
				"WWW-Authenticate",
				fmt.Sprintf("Bearer error=\"insufficient_scope\", scope=%q", strings.Join(requiredScopes, " ")),
			)
			c.JSON(http.StatusForbidden, httprouter.H{
				"status":  http.StatusForbidden,
				"error":   "forbidden",
				"message": "required scope is missing",
			})
			return false
		}
	}
	return true
}
