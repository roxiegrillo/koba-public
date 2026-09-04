package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/validator"
)

// ValidateToken validates a JWT token string and returns the user info
// Use this when you need to validate a token programmatically (e.g., from another service)
func (a *AuthMiddleware) ValidateToken(ctx context.Context, tokenString string) (*UserInfo, error) {
	principal, _, err := a.validateClaims(ctx, tokenString)
	if err != nil {
		return nil, err
	}
	return &principal, nil
}

func (a *AuthMiddleware) validateClaims(ctx context.Context, tokenString string) (Principal, *validator.ValidatedClaims, error) {
	if strings.TrimSpace(tokenString) == "" {
		return Principal{}, nil, fmt.Errorf("token is required")
	}
	validate := a.validate
	if validate == nil && a.Validator != nil {
		validate = TokenValidator(a.Validator.ValidateToken)
	}
	if validate == nil {
		return Principal{}, nil, fmt.Errorf("token validator is not configured")
	}
	validated, err := validate(ctx, tokenString)
	if err != nil {
		return Principal{}, nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := validated.(*validator.ValidatedClaims)
	if !ok || claims == nil {
		return Principal{}, nil, fmt.Errorf("invalid token claims")
	}
	userID := strings.TrimSpace(claims.RegisteredClaims.Subject)
	if userID == "" {
		return Principal{}, nil, fmt.Errorf("token subject is required")
	}
	principal := Principal{UserID: userID}
	switch customClaims := claims.CustomClaims.(type) {
	case *CustomClaims:
		if customClaims != nil {
			principal.Scope = customClaims.Scope
			principal.Permissions = append([]string(nil), customClaims.Permissions...)
		}
	case CustomClaims:
		principal.Scope = customClaims.Scope
		principal.Permissions = append([]string(nil), customClaims.Permissions...)
	}
	return principal, claims, nil
}

// ValidateRequest extracts and validates the JWT from an HTTP request's Authorization header
// Use this when you receive an HTTP request and need to validate the user
func (a *AuthMiddleware) ValidateRequest(r *http.Request) (*UserInfo, error) {
	tokenString, err := jwtmiddleware.AuthHeaderTokenExtractor(r)
	if err != nil {
		return nil, err
	}
	if tokenString == "" {
		return nil, fmt.Errorf("missing authorization header")
	}
	return a.ValidateToken(r.Context(), tokenString)
}

// IsAuthenticated checks if the request has a valid JWT token
// Returns true if authenticated, false otherwise
func (a *AuthMiddleware) IsAuthenticated(r *http.Request) bool {
	_, err := a.ValidateRequest(r)
	return err == nil
}

// GetUserID extracts the user ID from a request if authenticated
// Returns empty string if not authenticated
func (a *AuthMiddleware) GetUserID(r *http.Request) string {
	userInfo, err := a.ValidateRequest(r)
	if err != nil {
		return ""
	}
	return userInfo.UserID
}
