package middleware

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/rs/zerolog/log"
)

type Config struct {
	Domain   string
	Audience string
}

// CustomClaims contains custom data extracted from the JWT
type CustomClaims struct {
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions"`
}

// Validate satisfies the validator.CustomClaims interface
func (c CustomClaims) Validate(ctx context.Context) error {
	return nil
}

// Auth provides authentication functionality
type AuthMiddleware struct {
	Validator *validator.Validator
	validate  TokenValidator
	domain    string
	audience  string
}

// TokenValidator is injectable so authentication behavior can be tested
// without a network call to an OIDC provider.
type TokenValidator func(context.Context, string) (any, error)

// New creates a new Auth instance
func NewAuthMiddleware(config Config) (*AuthMiddleware, error) {
	logger := log.With().Str("component", "auth").Logger()

	domain := strings.TrimSpace(config.Domain)
	audience := strings.TrimSpace(config.Audience)
	if domain == "" {
		return nil, fmt.Errorf("auth domain is required")
	}
	if audience == "" {
		return nil, fmt.Errorf("auth audience is required")
	}
	issuerURL, err := url.Parse("https://" + domain)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to parse issuer URL")
		return nil, fmt.Errorf("failed to parse issuer URL: %w", err)
	}
	if issuerURL.Scheme != "https" || issuerURL.Host == "" || issuerURL.Host != domain ||
		issuerURL.User != nil || issuerURL.Path != "" || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, fmt.Errorf("auth domain must be a hostname with no scheme, path, query, fragment, or credentials")
	}
	issuerURL.Path = "/"

	logger.Info().
		Str("issuer", issuerURL.String()).
		Str("audience", audience).
		Msg("Initializing Auth")

	provider := jwks.NewCachingProvider(issuerURL, 5*time.Minute)

	jwtValidator, err := validator.New(
		provider.KeyFunc,
		validator.RS256,
		issuerURL.String(),
		[]string{audience},
		validator.WithCustomClaims(
			func() validator.CustomClaims {
				return &CustomClaims{}
			},
		),
	)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create JWT validator")
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	logger.Info().Msg("Auth initialized successfully")

	return &AuthMiddleware{
		Validator: jwtValidator,
		domain:    domain,
		audience:  audience,
	}, nil
}

// NewAuthMiddlewareWithTokenValidator creates an authentication middleware
// with an injected validator. It is intended for tests and non-Auth0 OIDC
// adapters which already provide fully validated claims.
func NewAuthMiddlewareWithTokenValidator(validate TokenValidator) (*AuthMiddleware, error) {
	if validate == nil {
		return nil, fmt.Errorf("token validator is required")
	}
	return &AuthMiddleware{validate: validate}, nil
}
