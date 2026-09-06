package koba

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
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

func TestReadinessOptions(t *testing.T) {
	check := func(context.Context) error { return nil }
	config, err := resolveAppConfig(
		WithReadinessCheck("postgres", check),
		WithReadinessCheck("kafka", check),
		WithReadinessTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.readinessChecks) != 2 || config.readinessTimeout != 5*time.Second {
		t.Fatalf("readiness config = %#v", config)
	}

	for _, test := range []struct {
		name    string
		options []AppOption
	}{
		{name: "empty name", options: []AppOption{WithReadinessCheck(" ", check)}},
		{name: "nil check", options: []AppOption{WithReadinessCheck("postgres", nil)}},
		{name: "duplicate check", options: []AppOption{WithReadinessCheck("postgres", check), WithReadinessCheck("postgres", check)}},
		{name: "invalid timeout", options: []AppOption{WithReadinessTimeout(0)}},
		{name: "duplicate timeout", options: []AppOption{WithReadinessTimeout(time.Second), WithReadinessTimeout(time.Second)}},
		{name: "disabled checks", options: []AppOption{WithoutDefaultProbes(), WithReadinessCheck("postgres", check)}},
		{name: "disabled timeout", options: []AppOption{WithReadinessTimeout(time.Second), WithoutDefaultProbes()}},
		{name: "duplicate disable", options: []AppOption{WithoutDefaultProbes(), WithoutDefaultProbes()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := resolveAppConfig(test.options...); err == nil {
				t.Fatal("resolveAppConfig() returned nil error")
			}
		})
	}
}

func TestReadinessOptionCanBeReusedConcurrently(t *testing.T) {
	option := WithReadinessCheck(" postgres ", func(context.Context) error { return nil })
	errors := make(chan error, 16)
	var waitGroup sync.WaitGroup
	for range 16 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			config, err := resolveAppConfig(option)
			if err == nil && (len(config.readinessChecks) != 1 || config.readinessChecks[0].Name != "postgres") {
				err = http.ErrAbortHandler
			}
			errors <- err
		}()
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
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
