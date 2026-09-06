// Package httpgin contains the Gin adapter used by the Koba app HTTP
// runtime. It must not define functional contracts or domain behavior.
package httpadapter

import (
	"net/http"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

type probeHandler interface {
	Handles(*http.Request) bool
	ServeHTTP(http.ResponseWriter, *http.Request)
}

// DefaultProbes serves framework defaults only when the application has not
// registered a matching route. It must be the first middleware in the chain so
// operational probes remain public and do not pollute business HTTP metrics.
func DefaultProbes(handler probeHandler) httprouter.HandlerFunc {
	return func(context *httprouter.Context) {
		if context.FullPath() != "" || !handler.Handles(context.Request) {
			return
		}
		handler.ServeHTTP(context.Writer, context.Request)
		context.Abort()
	}
}
