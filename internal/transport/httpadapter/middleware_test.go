package httpadapter

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

func TestRegistryAppliesMatchingHandlersInRegistrationOrder(t *testing.T) {
	registry := NewMiddlewareRegistry()
	items := regexp.MustCompile(`^/items`)
	registry.Add(items, headerMiddleware("one"))
	registry.Add(items, headerMiddleware("two"))
	registry.Add(regexp.MustCompile(`items`), headerMiddleware("three"))
	registry.Add(regexp.MustCompile(`^/other$`), headerMiddleware("not-used"))

	router := httprouter.New()
	registry.Apply("/items/123", router)
	router.GET("/items/123", func(context *httprouter.Context) {
		context.Header("X-Endpoint", "called")
		context.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/123", nil))
	if got, want := response.Header().Values("X-Middleware"), []string{"one", "two", "three"}; !equalStrings(got, want) {
		t.Fatalf("middleware order = %q, want %q", got, want)
	}
	if got := response.Header().Get("X-Endpoint"); got != "called" {
		t.Fatalf("endpoint header = %q, want called", got)
	}
}

func TestUseAbortsOnlyAfterStandardHandlerWritesResponse(t *testing.T) {
	for _, test := range []struct {
		name         string
		middleware   http.Handler
		wantStatus   int
		wantEndpoint bool
	}{
		{
			name: "continues without response",
			middleware: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("X-Middleware", "observed")
			}),
			wantStatus:   http.StatusNoContent,
			wantEndpoint: true,
		},
		{
			name: "aborts after response",
			middleware: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusAccepted)
			}),
			wantStatus:   http.StatusAccepted,
			wantEndpoint: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := httprouter.New()
			Use(router, test.middleware)
			endpointCalled := false
			router.GET("/resource", func(context *httprouter.Context) {
				endpointCalled = true
				context.Status(http.StatusNoContent)
			})

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resource", nil))
			if response.Code != test.wantStatus || endpointCalled != test.wantEndpoint {
				t.Fatalf(
					"response/endpoint = %d/%v, want %d/%v",
					response.Code,
					endpointCalled,
					test.wantStatus,
					test.wantEndpoint,
				)
			}
		})
	}
}

func headerMiddleware(value string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Add("X-Middleware", value)
	})
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
