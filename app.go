package koba

import (
	"net/http"
	"regexp"

	"github.com/emirpasic/gods/maps/linkedhashmap"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

type Empty struct{}

/*
KobaApp is used to register api's.
* Public functions are mapped to Http endpoints.
* The exception to this rules is a public Routes function which should return a `map[string]string`.
* If the function returns a value, the value is json serialzed in the response body
* If a function accepts a value, the body is serialized and passed to the function to process the request.
* Functions can contain a leading `context.Context` argument. This will not be serialzed and does not determine the http Method. Headers and Query arguments will be passed to `context.Context` argument.
*/
type KobaApp interface {
	/*AddApi gets an interfaces an create endpoints based on its public methods.*/
	AddApi(api interface{})
	Run()
}

/*
WithRouter exposes the router implementation: net/http's ServeMux, with its
method-aware patterns and path values, behind a request context and an ordered
middleware chain. Use only on special cases if a functionality is required but
not exposed by koba.
*/
type WithRouter interface {
	Router() *httprouter.Engine
}

// WithAPIDescriptions exposes the resolved operation descriptions registered
// on an app. It is used by adapters such as generated documentation without
// expanding the long-standing KobaApp interface.
type WithAPIDescriptions interface {
	RegisteredAPIDescriptions() []APIDescription
}

/*WithRoutesAPI are implemented by apis that required to override default naming conventions*/
type WithRoutesAPI interface {
	Routes() map[string]string
}

/*WithMiddlewareUse allows to define global middleware*/
type WithMiddlewareUse interface {
	Use(...http.Handler)
}

/*WithMiddlewareRegexUse allows to define endpoint level middleware*/
type WithMiddlewareRegexUse interface {
	UseGroup(*regexp.Regexp, ...http.Handler)
}

type kobaApp struct {
	r                                *httprouter.Engine
	middleware                       *linkedhashmap.Map
	authenticationMiddleware         http.Handler
	authenticationMiddlewareIsGlobal bool
	apis                             []APIDescription
}
