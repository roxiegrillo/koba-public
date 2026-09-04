package koba

import (
	"github.com/roxiegrillo/koba/v2/middleware"
)

func MakeRawApp() KobaApp {
	return MakeRawAppWithOptions()
}

// MakeRawAppWithOptions constructs a raw Koba app with explicit runtime
// options while preserving the original MakeRawApp function signature.
func MakeRawAppWithOptions(options ...AppOption) KobaApp {
	app, err := newKobaApp(options...)
	if err != nil {
		panic(err)
	}

	app.Use(&middleware.WithHttpContext{})
	return app
}

func MakeDefaultKobaApp() KobaApp {
	return MakeDefaultKobaAppWithOptions()
}

// MakeDefaultKobaAppWithOptions constructs a default Koba app
// with explicit runtime options while preserving the original constructor.
func MakeDefaultKobaAppWithOptions(options ...AppOption) KobaApp {
	app, err := newKobaApp(options...)
	if err != nil {
		panic(err)
	}
	app.Use(&middleware.WithHttpContext{})
	return app
}

// NewAuthenticatedApp constructs a globally authenticated application. It
// returns configuration errors so new services can fail startup cleanly.
func NewAuthenticatedApp(domain string, audience string) (KobaApp, error) {
	return NewAuthenticatedAppWithOptions(domain, audience)
}

// NewAuthenticatedAppWithOptions constructs an authenticated Koba app
// with explicit runtime options.
func NewAuthenticatedAppWithOptions(
	domain string,
	audience string,
	options ...AppOption,
) (KobaApp, error) {
	authMiddleware, err := middleware.NewAuthMiddleware(middleware.Config{
		Domain:   domain,
		Audience: audience,
	})
	if err != nil {
		return nil, err
	}
	authentication := authMiddleware.Middleware()
	configuredOptions := append([]AppOption(nil), options...)
	configuredOptions = append(configuredOptions, WithAuthenticationMiddleware(authentication))
	app, err := newKobaApp(configuredOptions...)
	if err != nil {
		return nil, err
	}
	app.Use(&middleware.WithHttpContext{})
	app.Use(authentication)
	app.authenticationMiddlewareIsGlobal = true
	return app, nil
}

// MakeAuthenticatedApp preserves the original constructor while making an
// invalid authentication configuration fail closed instead of silently
// starting an unauthenticated server.
func MakeAuthenticatedApp(domain string, audience string) KobaApp {
	return MakeAuthenticatedAppWithOptions(domain, audience)
}

// MakeAuthenticatedAppWithOptions constructs an authenticated Koba app
// with explicit runtime options and panics when configuration is invalid.
func MakeAuthenticatedAppWithOptions(
	domain string,
	audience string,
	options ...AppOption,
) KobaApp {
	app, err := NewAuthenticatedAppWithOptions(domain, audience, options...)
	if err != nil {
		panic(err)
	}
	return app
}

func newKobaApp(options ...AppOption) (*kobaApp, error) {
	config, err := resolveAppConfig(options...)
	if err != nil {
		return nil, err
	}

	app := &kobaApp{}
	if err := app.startApp(config); err != nil {
		return nil, err
	}
	return app, nil
}
