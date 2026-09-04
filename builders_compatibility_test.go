package koba

import "net/http"

// Compile-time assertions keep the original constructor signatures stable
// while the option-aware variants evolve independently.
var (
	_ func() KobaApp                                      = MakeRawApp
	_ func() KobaApp                                      = MakeDefaultKobaApp
	_ func(string, string) (KobaApp, error)               = NewAuthenticatedApp
	_ func(string, string) KobaApp                        = MakeAuthenticatedApp
	_ func(...AppOption) KobaApp                          = MakeRawAppWithOptions
	_ func(...AppOption) KobaApp                          = MakeDefaultKobaAppWithOptions
	_ func(string, string, ...AppOption) (KobaApp, error) = NewAuthenticatedAppWithOptions
	_ func(string, string, ...AppOption) KobaApp          = MakeAuthenticatedAppWithOptions
	_ func(http.Handler) AppOption                              = WithAuthenticationMiddleware
)
