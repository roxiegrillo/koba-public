// Package httprouter is the HTTP router the framework serves on. It is
// net/http's ServeMux, with its method-aware patterns and path values, behind
// a request context and an ordered middleware chain.
//
// A handler receives a *Context. Middleware is a handler that calls Next to
// run the rest of the chain, or Abort to stop it. An Engine is an
// http.Handler; routes are registered on it, or on a RouterGroup that shares a
// path prefix and a middleware list.
//
// Patterns follow ServeMux: "/items/{id}" names a path value, "/files/{rest...}"
// captures the remainder of the path, and a pattern that ends in "/" matches
// that path exactly. A segment written ":id" or, last, "*rest" is accepted as
// well and means the same as "{id}" and "{rest...}".
package httprouter

import (
	"net/http"
	"path"
	"strings"
)

// HandlerFunc is a handler, or a middleware, in a route's chain.
type HandlerFunc func(*Context)

// H is a shorthand for a JSON object literal.
type H map[string]any

// IRouter registers middleware and routes under a path prefix.
type IRouter interface {
	Use(middleware ...HandlerFunc)
	Group(prefix string, middleware ...HandlerFunc) *RouterGroup
	Handle(method, relativePath string, handlers ...HandlerFunc)
	GET(relativePath string, handlers ...HandlerFunc)
	POST(relativePath string, handlers ...HandlerFunc)
	PUT(relativePath string, handlers ...HandlerFunc)
	PATCH(relativePath string, handlers ...HandlerFunc)
	DELETE(relativePath string, handlers ...HandlerFunc)
}

// Engine is the router: an http.Handler that dispatches each request to the
// chain registered for its route, or to the no-route chain.
type Engine struct {
	RouterGroup
	mux     *http.ServeMux
	noRoute []HandlerFunc
}

var _ IRouter = (*Engine)(nil)

// New returns an engine with no middleware and no routes.
func New() *Engine {
	engine := &Engine{mux: http.NewServeMux()}
	engine.RouterGroup = RouterGroup{engine: engine, basePath: "/"}
	return engine
}

// Default returns an engine with the Logger and Recovery middleware installed.
func Default() *Engine {
	engine := New()
	engine.Use(Logger(), Recovery())
	return engine
}

// NoRoute sets the handlers that run, after the engine's middleware, for a
// request no route matches. When they write nothing the response is a plain
// 404.
func (engine *Engine) NoRoute(handlers ...HandlerFunc) {
	engine.noRoute = append([]HandlerFunc(nil), handlers...)
}

// Run serves the engine on addr, ":8080" when none is given, until the
// listener fails.
func (engine *Engine) Run(addr ...string) error {
	address := ":8080"
	if len(addr) > 0 && addr[0] != "" {
		address = addr[0]
	}
	server := &http.Server{Addr: address, Handler: engine}
	return server.ListenAndServe()
}

// ServeHTTP dispatches the request. A matched route runs its chain with the
// path values the pattern names; an unmatched request, including one whose
// path is known under another method, runs the engine's middleware followed by
// the NoRoute handlers.
func (engine *Engine) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	context := newContext(engine, writer, request)
	if _, pattern := engine.mux.Handler(request); pattern == "" {
		context.serveNoRoute()
		return
	}
	engine.mux.ServeHTTP(context.writer, request)
	context.writer.WriteHeaderNow()
}

// RouterGroup registers routes under a path prefix, each with the group's
// middleware ahead of its own handlers.
type RouterGroup struct {
	engine   *Engine
	basePath string
	handlers []HandlerFunc
}

var _ IRouter = (*RouterGroup)(nil)

// BasePath is the prefix every route of the group is registered under.
func (group *RouterGroup) BasePath() string {
	return group.basePath
}

// Use appends middleware to the group. It applies to the routes and groups
// registered afterwards, not to the ones registered before.
func (group *RouterGroup) Use(middleware ...HandlerFunc) {
	group.handlers = append(group.handlers, middleware...)
}

// Group returns a group whose prefix is this group's joined with prefix and
// whose middleware is this group's followed by the given one.
func (group *RouterGroup) Group(prefix string, middleware ...HandlerFunc) *RouterGroup {
	return &RouterGroup{
		engine:   group.engine,
		basePath: joinPaths(group.basePath, prefix),
		handlers: group.combine(middleware),
	}
}

// Handle registers handlers for method and relativePath, joined to the group's
// prefix. It panics, as ServeMux does, on a pattern that is invalid or that
// conflicts with one already registered.
func (group *RouterGroup) Handle(method, relativePath string, handlers ...HandlerFunc) {
	if method == "" {
		panic("httprouter: a route needs a method")
	}
	if len(handlers) == 0 {
		panic("httprouter: a route needs a handler")
	}
	route := toPattern(joinPaths(group.basePath, relativePath))
	chain := group.combine(handlers)
	engine := group.engine
	engine.mux.HandleFunc(method+" "+route.text, func(writer http.ResponseWriter, request *http.Request) {
		context, ok := contextOf(writer)
		if !ok {
			context = newContext(engine, writer, request)
			defer context.writer.WriteHeaderNow()
		}
		context.Request = request
		context.fullPath = route.text
		context.handlers = chain
		context.Params = make(Params, 0, len(route.names))
		for _, name := range route.names {
			context.Params = append(context.Params, Param{Key: name, Value: request.PathValue(name)})
		}
		context.Next()
	})
}

// GET registers handlers for GET requests to relativePath.
func (group *RouterGroup) GET(relativePath string, handlers ...HandlerFunc) {
	group.Handle(http.MethodGet, relativePath, handlers...)
}

// POST registers handlers for POST requests to relativePath.
func (group *RouterGroup) POST(relativePath string, handlers ...HandlerFunc) {
	group.Handle(http.MethodPost, relativePath, handlers...)
}

// PUT registers handlers for PUT requests to relativePath.
func (group *RouterGroup) PUT(relativePath string, handlers ...HandlerFunc) {
	group.Handle(http.MethodPut, relativePath, handlers...)
}

// PATCH registers handlers for PATCH requests to relativePath.
func (group *RouterGroup) PATCH(relativePath string, handlers ...HandlerFunc) {
	group.Handle(http.MethodPatch, relativePath, handlers...)
}

// DELETE registers handlers for DELETE requests to relativePath.
func (group *RouterGroup) DELETE(relativePath string, handlers ...HandlerFunc) {
	group.Handle(http.MethodDelete, relativePath, handlers...)
}

func (group *RouterGroup) combine(handlers []HandlerFunc) []HandlerFunc {
	merged := make([]HandlerFunc, 0, len(group.handlers)+len(handlers))
	merged = append(merged, group.handlers...)
	return append(merged, handlers...)
}

// WrapH adapts a standard handler to the chain. The handler writes the
// response itself; the chain continues afterwards unless it is aborted.
func WrapH(handler http.Handler) HandlerFunc {
	return func(context *Context) {
		handler.ServeHTTP(context.Writer, context.Request)
	}
}

// CreateTestContext returns a context that writes to w, with no request and
// no route, together with the engine it belongs to. Tests set Request and
// Params as the case needs.
func CreateTestContext(w http.ResponseWriter) (*Context, *Engine) {
	engine := New()
	return newContext(engine, w, nil), engine
}

// pattern is a route as ServeMux spells it, with the names of its path values.
type pattern struct {
	text  string
	names []string
}

// toPattern turns a registered path into a ServeMux pattern. A ":name"
// segment becomes "{name}", a last "*name" segment becomes "{name...}", and a
// path ending in "/" is anchored with "{$}" so it matches that path only.
func toPattern(absolutePath string) pattern {
	segments := strings.Split(absolutePath, "/")
	names := make([]string, 0, 2)
	for index, segment := range segments {
		switch {
		case strings.HasPrefix(segment, ":"):
			segments[index] = "{" + segment[1:] + "}"
		case strings.HasPrefix(segment, "*"):
			segments[index] = "{" + segment[1:] + "...}"
		}
		segment = segments[index]
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && segment != "{$}" {
			names = append(names, strings.TrimSuffix(segment[1:len(segment)-1], "..."))
		}
	}
	text := strings.Join(segments, "/")
	if strings.HasSuffix(text, "/") {
		text += "{$}"
	}
	return pattern{text: text, names: names}
}

// joinPaths joins a prefix and a relative path, keeping the relative path's
// trailing slash.
func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	joined := path.Join(absolutePath, relativePath)
	if strings.HasSuffix(relativePath, "/") && !strings.HasSuffix(joined, "/") {
		return joined + "/"
	}
	return joined
}
