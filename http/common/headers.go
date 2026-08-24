package http_common

import (
	"context"
	"net/http"
	"net/url"
)

func StandardHeaders() []string {
	return []string{}
}

type ContextKeys int

const (
	Headers ContextKeys = iota
	Query
	Request
	ResponseWriter
	PathParameters
)

// HeadersFromContext returns a copy of the HTTP request headers captured by
// the koba HTTP adapter. It returns an empty header map when the
// context does not contain request headers.
func HeadersFromContext(ctx context.Context) http.Header {
	headers, _ := ctx.Value(Headers).(http.Header)
	result := make(http.Header, len(headers))
	for name, values := range headers {
		result[name] = append([]string(nil), values...)
	}
	return result
}

// QueryFromContext returns a copy of the URL query parameters captured by the
// koba HTTP adapter. It returns empty values when the context
// does not contain query parameters.
func QueryFromContext(ctx context.Context) url.Values {
	query, _ := ctx.Value(Query).(url.Values)
	result := make(url.Values, len(query))
	for name, values := range query {
		result[name] = append([]string(nil), values...)
	}
	return result
}

// PathParametersFromContext returns a copy of the named route parameters
// captured by the koba HTTP adapter.
func PathParametersFromContext(ctx context.Context) map[string]string {
	parameters, _ := ctx.Value(PathParameters).(map[string]string)
	result := make(map[string]string, len(parameters))
	for name, value := range parameters {
		result[name] = value
	}
	return result
}

// ResponseWriterFromContext exposes the response writer for operations that
// need a non-default success status or response headers.
func ResponseWriterFromContext(ctx context.Context) (http.ResponseWriter, bool) {
	writer, ok := ctx.Value(ResponseWriter).(http.ResponseWriter)
	return writer, ok
}
