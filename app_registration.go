package koba

import (
	"net/http"
	"regexp"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
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
	if app.authenticationMiddleware == nil {
		for _, operation := range compiled.Description.Operations {
			if len(operation.ResolvedRequiredScopes()) == 0 {
				continue
			}
			panic("koba: operation " + operation.MethodName + " requires scopes but no authentication middleware is configured; use WithAuthenticationMiddleware")
		}
	}
	for _, operation := range compiled.Description.Operations {
		requiredScopes := operation.ResolvedRequiredScopes()
		routeName := operation.Route
		handler := makeOperationHandler(operation, compiled.Invokers[operation.MethodName])
		router := app.r.Group(routeName)
		if len(requiredScopes) > 0 && !app.authenticationMiddlewareIsGlobal {
			useWithRouter(router, app.authenticationMiddleware)
		}
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
