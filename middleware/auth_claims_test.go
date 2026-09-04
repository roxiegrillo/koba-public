package middleware

import "github.com/auth0/go-jwt-middleware/v2/validator"

func claims(subject string, permissions ...string) *validator.ValidatedClaims {
	return &validator.ValidatedClaims{
		RegisteredClaims: validator.RegisteredClaims{Subject: subject},
		CustomClaims: &CustomClaims{
			Scope:       "items:read",
			Permissions: permissions,
		},
	}
}
