package http

import (
	"net/http"

	client_middleware "github.com/roxiegrillo/koba/v2/http/client-middleware"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
)

// KobaClientBuilder configures the *http.Client that
// MakeDefaultKobaClient returns.
type KobaClientBuilder struct {
	// Middleware runs after the built-in header propagation, in order.
	Middleware []Middleware
	// Transport sends the requests; http.DefaultTransport when nil.
	Transport http.RoundTripper
}

// MakeDefaultKobaClient returns a client whose requests forward the
// standard headers of the incoming request found in their context, followed
// by the builder's own Middleware.
func (builder *KobaClientBuilder) MakeDefaultKobaClient() *http.Client {
	transport := builder.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	chain := make([]Middleware, 0, 1+len(builder.Middleware))
	chain = append(chain, &client_middleware.AddHeader{Headers: http_common.StandardHeaders()})
	chain = append(chain, builder.Middleware...)

	return &http.Client{
		Transport: &RequestClientMiddleware{Middleware: chain, RoundTripper: transport},
	}
}
