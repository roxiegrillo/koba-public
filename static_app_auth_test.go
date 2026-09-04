package koba_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/auth0/go-jwt-middleware/v2/validator"
	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/middleware"
)

func TestAuthenticationWrapsReflectedAndStaticPlans(t *testing.T) {
	t.Run("valid principal reaches operation context", func(t *testing.T) {
		auth, err := middleware.NewAuthMiddlewareWithTokenValidator(func(context.Context, string) (any, error) {
			return &validator.ValidatedClaims{
				RegisteredClaims: validator.RegisteredClaims{Subject: "auth0|static-user"},
				CustomClaims: &middleware.CustomClaims{
					Scope:       "read:fixtures create:fixtures",
					Permissions: []string{"unrelated:permission"},
				},
			}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		api := &staticfixture.API{}
		app := fb.MakeRawAppWithOptions(fb.WithAuthenticationMiddleware(auth.Middleware()))
		app.AddApi(api)

		request := httptest.NewRequest(http.MethodPost, "/fixtures", strings.NewReader(`{"name":"authorized"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()
		app.(fb.WithRouter).Router().ServeHTTP(response, request)
		if got, want := response.Code, http.StatusCreated; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
		if api.CreateCount != 1 || api.PrincipalID != "auth0|static-user" {
			t.Fatalf("API execution = count %d, principal %q", api.CreateCount, api.PrincipalID)
		}
	})

	t.Run("invalid token never invokes operation", func(t *testing.T) {
		auth, _ := middleware.NewAuthMiddlewareWithTokenValidator(func(context.Context, string) (any, error) {
			return nil, errors.New("rejected")
		})
		api := &staticfixture.API{}
		app := fb.MakeRawAppWithOptions(fb.WithAuthenticationMiddleware(auth.Middleware()))
		app.AddApi(api)

		request := httptest.NewRequest(http.MethodPost, "/fixtures", strings.NewReader(`{"name":"denied"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer invalid-token")
		response := httptest.NewRecorder()
		app.(fb.WithRouter).Router().ServeHTTP(response, request)
		if got, want := response.Code, http.StatusUnauthorized; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
		if api.CreateCount != 0 {
			t.Fatalf("protected API executed %d times", api.CreateCount)
		}
	})

	t.Run("valid token without required scope never invokes operation", func(t *testing.T) {
		auth, _ := middleware.NewAuthMiddlewareWithTokenValidator(func(context.Context, string) (any, error) {
			return &validator.ValidatedClaims{
				RegisteredClaims: validator.RegisteredClaims{Subject: "auth0|static-user"},
				CustomClaims: &middleware.CustomClaims{
					Scope:       "read:fixtures",
					Permissions: []string{"create:fixtures"},
				},
			}, nil
		})
		api := &staticfixture.API{}
		app := fb.MakeRawAppWithOptions(fb.WithAuthenticationMiddleware(auth.Middleware()))
		app.AddApi(api)

		request := httptest.NewRequest(http.MethodPost, "/fixtures", strings.NewReader(`{"name":"denied"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer valid-token")
		response := httptest.NewRecorder()
		app.(fb.WithRouter).Router().ServeHTTP(response, request)
		if got, want := response.Code, http.StatusForbidden; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
		if api.CreateCount != 0 {
			t.Fatalf("protected API executed %d times", api.CreateCount)
		}
	})
}
