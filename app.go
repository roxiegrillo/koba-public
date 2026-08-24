package koba

import (
	"net/http"
	"reflect"
	"regexp"

	"github.com/braintree/manners"

	"github.com/emirpasic/gods/maps/linkedhashmap"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/roxiegrillo/koba/v2/internal/httprouter"
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

// WithAPIDescriptions exposes the neutral contracts registered on an app. It
// is used by optional adapters such as generated documentation without
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
	r          *httprouter.Engine
	middleware *linkedhashmap.Map
	apis       []APIDescription
}

func (app *kobaApp) startApp() {
	app.r = httprouter.Default()
	app.middleware = linkedhashmap.New()
}

func (app *kobaApp) Router() *httprouter.Engine {
	return app.r
}

func (app *kobaApp) RegisteredAPIDescriptions() []APIDescription {
	return append([]APIDescription(nil), app.apis...)
}

func (app *kobaApp) Use(handlers ...http.Handler) {
	useWithRouter(app.r, handlers...)
}

func (app *kobaApp) UseGroup(r *regexp.Regexp, handlers ...http.Handler) {
	addMapMiddleware(r, app.middleware, handlers...)
}

func (app *kobaApp) AddApi(api interface{}) {
	compiled, err := CompileAPI(api)
	if err != nil {
		panic(err)
	}
	for _, operation := range compiled.Description.Operations {
		routeName := operation.Route
		handler := makeOperationHandler(operation, compiled.Invokers[operation.MethodName])
		router := app.r.Group(routeName)
		addMapRouter(routeName, router, app.middleware)
		switch operation.HTTPMethod {
		case http.MethodPost:
			router.POST("", handler)
		case http.MethodGet:
			router.GET("", handler)
		case http.MethodPut:
			router.PUT("", handler)
		case http.MethodPatch:
			router.PATCH("", handler)
		case http.MethodDelete:
			router.DELETE("", handler)
		default:
			panic("koba: unsupported resolved HTTP method " + operation.HTTPMethod)
		}
	}
	app.apis = append(app.apis, compiled.Description)
}

/*Run executes services that expose the api*/
func (app *kobaApp) Run() {
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()
	if err := manners.ListenAndServe(":8080", app.r); err != nil {
		panic(err.Error())
	}
}

type Pair[l any, r any] struct {
	Left  l
	Right r
}

func GetMethodValueGenericTypes(api interface{}) (result []*Pair[reflect.Method, reflect.Value]) {
	iType := reflect.TypeOf(api)
	for i := 0; i < iType.NumMethod(); i++ {
		method := iType.Method(i)
		// do not register private functions
		if !method.IsExported() || isNotExportable(method) {
			continue
		}
		methodValue := reflect.ValueOf(api).Method(i)
		result = append(result, &Pair[reflect.Method, reflect.Value]{
			Left:  method,
			Right: methodValue,
		})
	}
	return
}
