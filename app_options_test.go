package koba

import (
	"net/http"
	"testing"
)

func TestWithServiceNameRejectsInvalidOrDuplicateNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		options []AppOption
	}{
		{name: "empty", options: []AppOption{WithServiceName("")}},
		{name: "whitespace", options: []AppOption{WithServiceName("fenix items")}},
		{name: "uppercase", options: []AppOption{WithServiceName("FenixItems")}},
		{
			name: "duplicate",
			options: []AppOption{
				WithServiceName("fenix-items-api"),
				WithServiceName("other-service"),
			},
		},
		{name: "nil option", options: []AppOption{nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("MakeRawAppWithOptions() did not panic")
				}
			}()
			_ = MakeRawAppWithOptions(test.options...)
		})
	}
}

func TestWithAuthenticationMiddlewareRejectsNilOrDuplicateHandlers(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, test := range []struct {
		name    string
		options []AppOption
	}{
		{name: "nil", options: []AppOption{WithAuthenticationMiddleware(nil)}},
		{
			name: "duplicate",
			options: []AppOption{
				WithAuthenticationMiddleware(handler),
				WithAuthenticationMiddleware(handler),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("MakeRawAppWithOptions() did not panic")
				}
			}()
			_ = MakeRawAppWithOptions(test.options...)
		})
	}
}
