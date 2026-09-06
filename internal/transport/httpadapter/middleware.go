// Package httpadapter contains the HTTP transport adapter used by Koba apps.
// It must not define functional contracts or domain behavior.
package httpadapter

import (
	"net/http"
	"regexp"

	"github.com/emirpasic/gods/maps/linkedhashmap"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

// MiddlewareRegistry stores route-scoped middleware in registration order.
// The registry and its backing collection are private transport details.
type MiddlewareRegistry struct {
	handlers *linkedhashmap.Map
}

// NewMiddlewareRegistry constructs an empty route-scoped middleware registry.
func NewMiddlewareRegistry() *MiddlewareRegistry {
	return &MiddlewareRegistry{handlers: linkedhashmap.New()}
}

// Add associates middleware with a route pattern. Reusing the same pattern
// instance appends handlers in registration order.
func (registry *MiddlewareRegistry) Add(pattern *regexp.Regexp, handlers ...http.Handler) {
	var registered []http.Handler
	if existing, ok := registry.handlers.Get(pattern); ok {
		registered = append(registered, existing.([]http.Handler)...)
	}
	registered = append(registered, handlers...)
	registry.handlers.Put(pattern, registered)
}

// Apply installs every registered middleware group that matches route.
func (registry *MiddlewareRegistry) Apply(route string, router httprouter.IRouter) {
	registry.handlers.Each(func(key interface{}, value interface{}) {
		pattern := key.(*regexp.Regexp)
		if pattern.MatchString(route) {
			Use(router, value.([]http.Handler)...)
		}
	})
}

// Use adapts standard HTTP middleware to an HTTP router.
func Use(router httprouter.IRouter, handlers ...http.Handler) {
	adapted := make([]httprouter.HandlerFunc, 0, len(handlers))
	for _, handler := range handlers {
		adapted = append(adapted, adapt(handler))
	}
	router.Use(adapted...)
}

func adapt(handler http.Handler) httprouter.HandlerFunc {
	return func(context *httprouter.Context) {
		initialStatus := context.Writer.Status()
		initialSize := context.Writer.Size()

		handler.ServeHTTP(context.Writer, context.Request)

		finalStatus := context.Writer.Status()
		finalSize := context.Writer.Size()
		if finalStatus != initialStatus || finalSize > initialSize {
			context.Abort()
		}
	}
}
