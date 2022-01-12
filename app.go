// Package koba serves an API over HTTP. An API is an ordinary Go
// value: each of its exported methods is an endpoint, served at a route named
// after the method, and the value it returns is the response.
package koba

import (
	"github.com/roxiegrillo/koba/v2/internal/httprouter"
)

// App serves APIs over HTTP. An API is any value; each of its exported methods
// becomes an endpoint at the route named after the method (see RouteOf),
// unless the API implements WithRoutesAPI and names another one.
type App interface {
	// AddApi registers api: each exported method becomes an endpoint. It
	// panics when a method has a shape no endpoint can serve.
	AddApi(api any)
	// Run serves the endpoints on addr until the listener fails.
	Run(addr string) error
}

// WithRouter is implemented by an app that gives access to the router it
// serves on, for the rare need this package does not otherwise cover.
type WithRouter interface {
	Router() *httprouter.Engine
}

// Routes maps a method name to the route its endpoint is served at.
type Routes map[string]string

// WithRoutesAPI is implemented by an API that chooses the routes of some of
// its endpoints instead of the ones named after its methods. Routes itself is
// not an endpoint.
type WithRoutesAPI interface {
	Routes() Routes
}

// MakeRawApp returns an app with the router and nothing else.
func MakeRawApp() App {
	return newApp()
}

type app struct {
	router *httprouter.Engine
}

func newApp() *app {
	router := httprouter.New()
	router.Use(httprouter.Recovery())
	return &app{router: router}
}

func (served *app) Router() *httprouter.Engine {
	return served.router
}

func (served *app) AddApi(api any) {
	endpoints, err := endpointsOf(api)
	if err != nil {
		panic(err)
	}
	for _, endpoint := range endpoints {
		served.router.Handle(endpoint.httpMethod, endpoint.route, endpoint.handler())
	}
}

func (served *app) Run(addr string) error {
	return served.router.Run(addr)
}
