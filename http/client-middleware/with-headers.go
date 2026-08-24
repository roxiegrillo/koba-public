package client_middleware

import (
	"net/http"
	"strings"
)

// WithHeaders is a client Middleware that sets fixed headers on every request.
type WithHeaders struct {
	// Headers holds the values to set. A Host entry sets the request's Host,
	// its last value winning; an entry without values is ignored.
	Headers http.Header
}

// Request applies Headers to the request.
func (fixed *WithHeaders) Request(request *http.Request) error {
	for name, values := range fixed.Headers {
		if !strings.EqualFold(name, "Host") {
			replaceHeader(request.Header, name, values)
			continue
		}
		if len(values) > 0 {
			request.Host = values[len(values)-1]
		}
	}
	return nil
}

// Response does nothing.
func (*WithHeaders) Response(*http.Response) error { return nil }
