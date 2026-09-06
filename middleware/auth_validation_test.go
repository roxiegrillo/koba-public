package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateTokenRejectsInvalidValidatorResults(t *testing.T) {
	tests := []struct {
		name     string
		validate TokenValidator
		want     string
	}{
		{name: "validator error", validate: func(context.Context, string) (any, error) { return nil, errors.New("rejected") }, want: "invalid token"},
		{name: "nil result", validate: func(context.Context, string) (any, error) { return nil, nil }, want: "invalid token claims"},
		{name: "wrong result type", validate: func(context.Context, string) (any, error) { return "claims", nil }, want: "invalid token claims"},
		{name: "missing expiration", validate: func(context.Context, string) (any, error) {
			validated := claims("auth0|user-1")
			validated.RegisteredClaims.Expiry = 0
			return validated, nil
		}, want: "expiration is required"},
		{name: "empty subject", validate: func(context.Context, string) (any, error) { return claims(""), nil }, want: "subject is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			auth, err := NewAuthMiddlewareWithTokenValidator(test.validate)
			if err != nil {
				t.Fatalf("constructor returned an error: %v", err)
			}
			_, err = auth.ValidateToken(context.Background(), "token")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateToken() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestValidateRequestRejectsMultipleAuthorizationHeaders(t *testing.T) {
	validationCalls := 0
	auth, err := NewAuthMiddlewareWithTokenValidator(func(_ context.Context, _ string) (any, error) {
		validationCalls++
		return claims("auth0|user-1"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Add("Authorization", "Bearer first-token")
	request.Header.Add("Authorization", "Bearer second-token")

	if _, err := auth.ValidateRequest(request); err == nil || !strings.Contains(err.Error(), "multiple authorization headers") {
		t.Fatalf("ValidateRequest() error = %v, want multiple-header error", err)
	}
	if validationCalls != 0 {
		t.Fatalf("validation calls = %d, want 0", validationCalls)
	}
}

func TestValidateRequestRejectsNilRequest(t *testing.T) {
	auth, err := NewAuthMiddlewareWithTokenValidator(func(_ context.Context, _ string) (any, error) {
		return claims("auth0|user-1"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ValidateRequest(nil); err == nil || !strings.Contains(err.Error(), "request is required") {
		t.Fatalf("ValidateRequest(nil) error = %v, want request-required error", err)
	}
}

func TestValidateRequestUsesStrictCaseInsensitiveBearerHeader(t *testing.T) {
	auth, err := NewAuthMiddlewareWithTokenValidator(func(_ context.Context, token string) (any, error) {
		if token != "valid-token" {
			return nil, errors.New("unexpected token")
		}
		return claims("auth0|user-1", "items:read"), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := httptest.NewRequest(http.MethodGet, "/", nil)
	valid.Header.Set("Authorization", "bearer valid-token")
	principal, err := auth.ValidateRequest(valid)
	if err != nil || principal.UserID != "auth0|user-1" {
		t.Fatalf("ValidateRequest() = %#v, %v", principal, err)
	}
	for _, header := range []string{"", "Basic value", "Bearer", "Bearer one two"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", header)
		if _, err := auth.ValidateRequest(request); err == nil {
			t.Fatalf("ValidateRequest(%q) succeeded", header)
		}
	}
}
