package koba

import (
	"net/http"
	"regexp"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"

	internalhttpadapter "github.com/roxiegrillo/koba/v2/internal/transport/httpadapter"
)

func (app *kobaApp) Router() *httprouter.Engine {
	return app.r
}

// ServeHTTP exposes the Koba app through Go's transport-neutral HTTP
// contract. Services can configure their own http.Server lifecycle without
// depending on the framework's internal router implementation.
func (app *kobaApp) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	app.r.ServeHTTP(writer, request)
}

func (app *kobaApp) RegisteredAPIDescriptions() []APIDescription {
	return append([]APIDescription(nil), app.apis...)
}

func (app *kobaApp) DefaultProbesEnabled() bool {
	return !app.defaultProbesDisabled
}

func (app *kobaApp) Use(handlers ...http.Handler) {
	internalhttpadapter.Use(app.r, handlers...)
}

func (app *kobaApp) UseGroup(r *regexp.Regexp, handlers ...http.Handler) {
	app.middleware.Add(r, handlers...)
}

func (app *kobaApp) AddApi(api interface{}) {
	compiled, err := CompileAPI(api)
	if err != nil {
		panic(err)
	}
	if app.authenticationMiddleware == nil {
		for _, operation := range compiled.Description.Operations {
			if operation.HasKafkaConsumerTransport() {
				continue
			}
			if !operation.RequiresAuthentication() {
				continue
			}
			panic("koba: operation " + operation.MethodName + " requires authentication but no authentication middleware is configured; use WithAuthenticationMiddleware")
		}
	}
	for _, operation := range compiled.Description.Operations {
		if operation.HasKafkaConsumerTransport() {
			continue
		}
		routeName := operation.Route
		handler := makeOperationHandler(operation, compiled.Invokers[operation.MethodName])
		router := app.r.Group(routeName)
		if operation.RequiresAuthentication() && !app.authenticationMiddlewareIsGlobal {
			internalhttpadapter.Use(router, app.authenticationMiddleware)
		}
		app.middleware.Apply(routeName, router)
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
