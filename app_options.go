package koba

import (
	"fmt"
	"net/http"
)

type appConfig struct {
	serviceName                 string
	serviceNameSet              bool
	authenticationMiddleware    http.Handler
	authenticationMiddlewareSet bool
}

// WithAuthenticationMiddleware configures the middleware used to authenticate
// operations whose APIContract declares required scopes. Legacy
// OperationDocumentation.RequiredScopes declarations remain supported. Unlike
// KobaApp.Use, this middleware is attached only to scoped operations.
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

func resolveAppConfig(options ...AppOption) (appConfig, error) {
	config := appConfig{}
	for index, option := range options {
		if option == nil {
			return appConfig{}, fmt.Errorf("koba: app option %d is nil", index)
		}
		if err := option.apply(&config); err != nil {
			return appConfig{}, err
		}
	}
	return config, nil
}
