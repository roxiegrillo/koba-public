package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

func TestKobaAppProvidesPublicDefaultProbes(t *testing.T) {
	app := fb.MakeRawApp()
	authCalls := 0
	app.(fb.WithMiddlewareUse).Use(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		authCalls++
		writer.WriteHeader(http.StatusUnauthorized)
	}))

	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/healthz", body: `{"status":"ok"}`},
		{path: "/readyz", body: `{"status":"ready"}`},
	} {
		response := httptest.NewRecorder()
		app.(http.Handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != test.body {
			t.Fatalf("GET %s = %d %q", test.path, response.Code, response.Body.String())
		}
	}
	if authCalls != 0 {
		t.Fatalf("application middleware handled %d default probes", authCalls)
	}
	if !app.(fb.WithOperationalProbes).DefaultProbesEnabled() {
		t.Fatal("default probes are served but reported as disabled")
	}
	if descriptions := app.(fb.WithAPIDescriptions).RegisteredAPIDescriptions(); len(descriptions) != 0 {
		t.Fatalf("framework probes appeared as service-owned APIs: %#v", descriptions)
	}

	response := httptest.NewRecorder()
	app.(http.Handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusUnauthorized || authCalls != 1 {
		t.Fatalf("ordinary request = status %d, middleware calls %d", response.Code, authCalls)
	}
}

func TestDefaultReadinessUsesConfiguredChecks(t *testing.T) {
	checkCalls := 0
	app := fb.MakeRawAppWithOptions(fb.WithReadinessCheck("postgres", func(context.Context) error {
		checkCalls++
		return errors.New("database unavailable")
	}))

	health := httptest.NewRecorder()
	app.(http.Handler).ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || checkCalls != 0 {
		t.Fatalf("health = status %d, readiness calls %d", health.Code, checkCalls)
	}

	ready := httptest.NewRecorder()
	app.(http.Handler).ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable || strings.TrimSpace(ready.Body.String()) != `{"status":"not_ready"}` || checkCalls != 1 {
		t.Fatalf("ready = status %d, body %q, calls %d", ready.Code, ready.Body.String(), checkCalls)
	}
}

type explicitProbeAPI struct{}

func (*explicitProbeAPI) Routes() map[string]string {
	return map[string]string{"Health": "/healthz", "Ready": "/readyz"}
}

func (*explicitProbeAPI) Health() map[string]string {
	return map[string]string{"status": "custom-health"}
}

func (*explicitProbeAPI) Ready() map[string]string {
	return map[string]string{"status": "custom-ready"}
}

func (*explicitProbeAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Health": {OperationID: "custom_health", Public: true},
		"Ready":  {OperationID: "custom_ready", Public: true},
	}}
}

func TestExplicitProbeRoutesOverrideDefaults(t *testing.T) {
	app := fb.MakeRawAppWithOptions(fb.WithReadinessCheck("unused", func(context.Context) error {
		return errors.New("must not run")
	}))
	app.AddApi(&explicitProbeAPI{})

	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/healthz", body: `{"status":"custom-health"}`},
		{path: "/readyz", body: `{"status":"custom-ready"}`},
	} {
		response := httptest.NewRecorder()
		app.(http.Handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != test.body {
			t.Fatalf("GET %s = %d %q", test.path, response.Code, response.Body.String())
		}
	}
	descriptions := app.(fb.WithAPIDescriptions).RegisteredAPIDescriptions()
	assertDescribedProbeRoutes(t, descriptions, map[string]string{
		"/healthz": "custom_health",
		"/readyz":  "custom_ready",
	})
}

func TestDefaultProbesCanBeDisabled(t *testing.T) {
	app := fb.MakeRawAppWithOptions(fb.WithoutDefaultProbes())
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		app.(http.Handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, response.Code)
		}
	}
	if descriptions := app.(fb.WithAPIDescriptions).RegisteredAPIDescriptions(); len(descriptions) != 0 {
		t.Fatalf("disabled probes appeared in API descriptions: %#v", descriptions)
	}
	if app.(fb.WithOperationalProbes).DefaultProbesEnabled() {
		t.Fatal("disabled probes are reported as enabled")
	}
}

func assertDescribedProbeRoutes(t *testing.T, descriptions []fb.APIDescription, want map[string]string) {
	t.Helper()
	seen := make(map[string]string)
	for _, description := range descriptions {
		for _, operation := range description.Operations {
			if _, tracked := want[operation.Route]; tracked && operation.HTTPMethod == http.MethodGet {
				if previous, duplicate := seen[operation.Route]; duplicate {
					t.Fatalf("GET %s described more than once (%q and %q)", operation.Route, previous, operation.OperationID)
				}
				seen[operation.Route] = operation.OperationID
			}
		}
	}
	for route, operationID := range want {
		if got := seen[route]; got != operationID {
			t.Errorf("GET %s operation ID = %q, want %q", route, got, operationID)
		}
	}
}
