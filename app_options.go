package koba

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalhealth "github.com/roxiegrillo/koba/v2/internal/health"
)

type appConfig struct {
	serviceName                 string
	serviceNameSet              bool
	authenticationMiddleware    http.Handler
	authenticationMiddlewareSet bool
	readinessChecks             []internalhealth.Check
	readinessTimeout            time.Duration
	readinessTimeoutSet         bool
	defaultProbesDisabled       bool
	defaultProbesOptionSet      bool
}

// ReadinessCheckFunc verifies one dependency required before the service can
// receive traffic. It must honor cancellation and deadlines from ctx.
type ReadinessCheckFunc func(context.Context) error

// WithAuthenticationMiddleware configures the middleware used to authenticate
// operations whose APIContract declares a Subject binding or required scopes.
// Legacy OperationDocumentation.RequiredScopes declarations remain supported.
// Unlike KobaApp.Use, this middleware is attached only to authenticated
// operations.
func WithAuthenticationMiddleware(handler http.Handler) AppOption {
	return appOptionFunc(func(config *appConfig) error {
		if config.authenticationMiddlewareSet {
			return fmt.Errorf("koba: authentication middleware is configured more than once")
		}
		if handler == nil {
			return fmt.Errorf("koba: authentication middleware is nil")
		}
		config.authenticationMiddleware = handler
		config.authenticationMiddlewareSet = true
		return nil
	})
}

// AppOption configures a Koba app at construction time.
type AppOption interface {
	apply(*appConfig) error
}

type appOptionFunc func(*appConfig) error

func (option appOptionFunc) apply(config *appConfig) error {
	return option(config)
}

// WithServiceName enables the standard Koba HTTP metrics and
// sets their service label. The value should be the service repository name.
func WithServiceName(name string) AppOption {
	return appOptionFunc(func(config *appConfig) error {
		if config.serviceNameSet {
			return fmt.Errorf("koba: service name is configured more than once")
		}
		config.serviceName = name
		config.serviceNameSet = true
		return nil
	})
}

// WithReadinessCheck adds one dependency check to the default /readyz probe.
// Checks run in registration order under one shared readiness timeout, and all
// checks must pass. The default /healthz probe never runs dependency checks.
func WithReadinessCheck(name string, check ReadinessCheckFunc) AppOption {
	normalizedName := strings.TrimSpace(name)
	return appOptionFunc(func(config *appConfig) error {
		if normalizedName == "" {
			return fmt.Errorf("koba: readiness check name is required")
		}
		if check == nil {
			return fmt.Errorf("koba: readiness check %q is nil", normalizedName)
		}
		for _, existing := range config.readinessChecks {
			if existing.Name == normalizedName {
				return fmt.Errorf("koba: readiness check %q is configured more than once", normalizedName)
			}
		}
		config.readinessChecks = append(config.readinessChecks, internalhealth.Check{
			Name: normalizedName,
			Run:  func(ctx context.Context) error { return check(ctx) },
		})
		return nil
	})
}

// WithReadinessTimeout overrides the two-second deadline shared by checks for
// one /readyz request.
func WithReadinessTimeout(timeout time.Duration) AppOption {
	return appOptionFunc(func(config *appConfig) error {
		if config.readinessTimeoutSet {
			return fmt.Errorf("koba: readiness timeout is configured more than once")
		}
		if timeout <= 0 {
			return fmt.Errorf("koba: readiness timeout must be positive")
		}
		config.readinessTimeout = timeout
		config.readinessTimeoutSet = true
		return nil
	})
}

// WithoutDefaultProbes disables the framework-owned /healthz and /readyz
// fallbacks. Applications should use this only when an enclosing runtime owns
// the operational endpoints.
func WithoutDefaultProbes() AppOption {
	return appOptionFunc(func(config *appConfig) error {
		if config.defaultProbesOptionSet {
			return fmt.Errorf("koba: default probes option is configured more than once")
		}
		config.defaultProbesDisabled = true
		config.defaultProbesOptionSet = true
		return nil
	})
}

func resolveAppConfig(options ...AppOption) (appConfig, error) {
	config := appConfig{readinessTimeout: internalhealth.DefaultReadinessTimeout}
	for index, option := range options {
		if option == nil {
			return appConfig{}, fmt.Errorf("koba: app option %d is nil", index)
		}
		if err := option.apply(&config); err != nil {
			return appConfig{}, err
		}
	}
	if config.defaultProbesDisabled && (len(config.readinessChecks) != 0 || config.readinessTimeoutSet) {
		return appConfig{}, fmt.Errorf("koba: readiness configuration cannot be used when default probes are disabled")
	}
	return config, nil
}
