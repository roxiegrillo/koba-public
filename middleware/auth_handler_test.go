package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
)

func TestResolverHandlerFailsClosedAndSetsTypedPrincipal(t *testing.T) {
	t.Run("invalid claims", func(t *testing.T) {
		auth, _ := NewAuthMiddlewareWithTokenValidator(func(context.Context, string) (any, error) {
			return "not claims", nil
		})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		auth.Middleware().ServeHTTP(response, request)
		if got, want := response.Code, http.StatusUnauthorized; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if response.Header().Get("WWW-Authenticate") != "Bearer" || !strings.Contains(response.Body.String(), `"error":"unauthorized"`) {
			t.Fatalf("unauthorized response headers/body = %#v/%s", response.Header(), response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
		if _, ok := PrincipalFromContext(request.Context()); ok {
			t.Fatal("invalid request received a Principal")
		}
	})

	t.Run("valid claims", func(t *testing.T) {
		validatedClaims := claims("auth0|user-2", "items:write")
		auth, _ := NewAuthMiddlewareWithTokenValidator(func(context.Context, string) (any, error) {
			return validatedClaims, nil
		})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		auth.Middleware().ServeHTTP(response, request)
		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want untouched %d", got, want)
		}
		principal, ok := PrincipalFromContext(request.Context())
		if !ok || principal.UserID != "auth0|user-2" || len(principal.Permissions) != 1 {
			t.Fatalf("PrincipalFromContext() = %#v, %v", principal, ok)
		}
		if got := request.Context().Value("user_id"); got != "auth0|user-2" {
			t.Fatalf("legacy user_id = %#v", got)
		}
		if got := request.Context().Value(jwtmiddleware.ContextKey{}); got != validatedClaims {
			t.Fatalf("JWT context claims = %#v", got)
		}
	})
}

func TestOptionalMiddlewareAllowsAnonymousRequestsAndFailsClosedOnBadCredentials(t *testing.T) {
	validatedClaims := claims("auth0|optional-user", "items:read")
	validationCalls := 0
	auth, err := NewAuthMiddlewareWithTokenValidator(func(_ context.Context, token string) (any, error) {
		validationCalls++
		if token != "valid-token" {
			return nil, fmt.Errorf("rejected token")
		}
		return validatedClaims, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("absent header remains anonymous", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()

		auth.OptionalMiddleware().ServeHTTP(response, request)

		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want untouched %d", got, want)
		}
		if validationCalls != 0 {
			t.Fatalf("validation calls = %d, want 0", validationCalls)
		}
		if _, ok := PrincipalFromContext(request.Context()); ok {
			t.Fatal("anonymous request received a Principal")
		}
	})

	t.Run("valid bearer token sets the same authentication context as strict middleware", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()

		auth.OptionalMiddleware().ServeHTTP(response, request)

		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want untouched %d", got, want)
		}
		principal, ok := PrincipalFromContext(request.Context())
		if !ok || principal.UserID != "auth0|optional-user" || len(principal.Permissions) != 1 {
			t.Fatalf("PrincipalFromContext() = %#v, %v", principal, ok)
		}
		if got := request.Context().Value(jwtmiddleware.ContextKey{}); got != validatedClaims {
			t.Fatalf("JWT context claims = %#v", got)
		}
		if got := request.Context().Value("user_id"); got != "auth0|optional-user" {
			t.Fatalf("legacy user_id = %#v", got)
		}
	})

	for _, test := range []struct {
		name   string
		header string
	}{
		{name: "malformed header", header: "Basic credentials"},
		{name: "invalid bearer token", header: "Bearer invalid-token"},
		{name: "present empty header", header: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header["Authorization"] = []string{test.header}
			response := httptest.NewRecorder()

			auth.OptionalMiddleware().ServeHTTP(response, request)

			if got, want := response.Code, http.StatusUnauthorized; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if _, ok := PrincipalFromContext(request.Context()); ok {
				t.Fatal("unauthorized request received a Principal")
			}
		})
	}
}
