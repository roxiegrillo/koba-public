package koba

import (
	"net/http"
	"regexp"

	"github.com/emirpasic/gods/maps/linkedhashmap"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

func addMapMiddleware(r *regexp.Regexp, m *linkedhashmap.Map, handlers ...http.Handler) {
	var newHandlers []http.Handler
	if h, ok := m.Get(r); ok {
		hOld := h.([]http.Handler)
		newHandlers = append(newHandlers, hOld...)
	}
	newHandlers = append(newHandlers, handlers...)
	m.Put(r, newHandlers)
}

func asRouterHandler(h http.Handler) httprouter.HandlerFunc {
	return func(c *httprouter.Context) {
		// Save the initial status before handler execution
		initialStatus := c.Writer.Status()
		initialSize := c.Writer.Size()

		// Execute the handler
		h.ServeHTTP(c.Writer, c.Request)

		// Check if handler wrote to the response or set any status
		finalStatus := c.Writer.Status()
		finalSize := c.Writer.Size()

		// If status changed or content was written, abort the chain
		if finalStatus != initialStatus || finalSize > initialSize {
			c.Abort()
		}
	}
}

func addMapRouter(name string, router httprouter.IRouter, m *linkedhashmap.Map) {
	m.Each(func(key interface{}, value interface{}) {
		r := key.(*regexp.Regexp)
		if r.MatchString(name) {
			middleware := value.([]http.Handler)
			useWithRouter(router, middleware...)
		}
	})
}

func useWithRouter(router httprouter.IRouter, handlers ...http.Handler) {
	var h []httprouter.HandlerFunc

	for _, handle := range handlers {
		h = append(h, asRouterHandler(handle))
	}
	router.Use(h...)
}
