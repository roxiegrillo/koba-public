package client_middleware

import (
	"net/http"

	http_common "github.com/roxiegrillo/koba/v2/http/common"
)

// AddHeader is a client Middleware that forwards chosen headers of the
// incoming request, as captured in the outgoing request's context, onto the
// outgoing request.
type AddHeader struct {
	// Headers names the headers to forward.
	Headers []string
}

// Request replaces each named header that the incoming request carried. A
// header the incoming request did not carry is left as it is.
func (forward *AddHeader) Request(request *http.Request) error {
	incoming, captured := request.Context().Value(http_common.Headers).(http.Header)
	if !captured {
		return nil
	}
	for _, name := range forward.Headers {
		replaceHeader(request.Header, name, incoming.Values(name))
	}
	return nil
}

// Response does nothing.
func (*AddHeader) Response(*http.Response) error { return nil }

// replaceHeader sets name to values, keeping what is there when values is
// empty.
func replaceHeader(header http.Header, name string, values []string) {
	if len(values) == 0 {
		return
	}
	header.Del(name)
	for _, value := range values {
		header.Add(name, value)
	}
}
