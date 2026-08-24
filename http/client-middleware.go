package http

import "net/http"

// Middleware observes and may alter the requests an http.Client sends and the
// responses it receives. An error from either method fails the round trip.
type Middleware interface {
	Request(request *http.Request) error
	Response(response *http.Response) error
}

// RequestClientMiddleware is an http.RoundTripper that wraps RoundTripper with
// a chain of Middleware.
type RequestClientMiddleware struct {
	Middleware   []Middleware
	RoundTripper http.RoundTripper
}

// RoundTrip passes the request through every Middleware in order, sends it
// with RoundTripper (http.DefaultTransport when nil), then passes the response
// back through the chain in reverse order, so the first middleware sees the
// request first and the response last.
//
// A failing request phase stops before anything is sent. A transport error is
// returned as is, without a response phase. A failing response phase closes
// the response body and returns no response.
func (chain *RequestClientMiddleware) RoundTrip(request *http.Request) (*http.Response, error) {
	for _, middleware := range chain.Middleware {
		if err := middleware.Request(request); err != nil {
			return nil, err
		}
	}

	transport := chain.RoundTripper
	if transport == nil {
		transport = http.DefaultTransport
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		return response, err
	}

	for index := len(chain.Middleware) - 1; index >= 0; index-- {
		if err := chain.Middleware[index].Response(response); err != nil {
			if response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, err
		}
	}
	return response, nil
}

// Request adapts a function to a Middleware that acts on requests only.
type Request func(request *http.Request) error

// Request calls the function.
func (hook Request) Request(request *http.Request) error { return hook(request) }

// Response leaves the response untouched.
func (Request) Response(*http.Response) error { return nil }

// Response adapts a function to a Middleware that acts on responses only.
type Response func(response *http.Response) error

// Request leaves the request untouched.
func (Response) Request(*http.Request) error { return nil }

// Response calls the function.
func (hook Response) Response(response *http.Response) error { return hook(response) }
