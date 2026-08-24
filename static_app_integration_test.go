package koba_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/kobaapp"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
	"github.com/roxiegrillo/koba/v2/openapi"
)

func staticFixtureDefinition() kobaapp.Definition {
	return kobaapp.Definition{
		ID:             "static-fixture",
		APIs:           []any{(*staticfixture.API)(nil)},
		OpenAPI:        openapi.Config{Title: "Static Fixture API", Version: "1.0.0"},
		OpenAPIOutput:  "internal/staticfixture/openapi.json",
		ManifestOutput: "internal/staticfixture/koba.lock.json",
		StaticTargets: []kobaapp.StaticTarget{{
			PackageName: "staticfixture",
			PackagePath: "github.com/roxiegrillo/koba/v2/internal/staticfixture",
			Output:      "internal/staticfixture/zz_koba.gen.go",
			APIs:        []any{(*staticfixture.API)(nil)},
		}},
	}
}

func TestStaticFixtureArtifactsAreCurrent(t *testing.T) {
	if err := kobaapp.Check(staticFixtureDefinition()); err != nil {
		t.Fatalf("kobaapp.Check() returned an error: %v", err)
	}
}

func TestStaticFixtureExecutionPlan(t *testing.T) {
	api := &staticfixture.API{}
	_, hasStaticProvider := any(api).(fb.WithCompiledAPI)
	if hasStaticProvider != staticProviderExpected {
		t.Fatalf("static provider present = %v, want %v", hasStaticProvider, staticProviderExpected)
	}

	compiled, err := fb.CompileAPI(api)
	if err != nil {
		t.Fatalf("CompileAPI() returned an error: %v", err)
	}
	operations := make(map[string]fb.OperationDescription, len(compiled.Description.Operations))
	for _, operation := range compiled.Description.Operations {
		operations[operation.MethodName] = operation
	}
	if got, want := operations["Create"].ResolvedSuccessStatus(), http.StatusCreated; got != want {
		t.Fatalf("Create status = %d, want %d", got, want)
	}
	if got, want := operations["Delete"].HTTPMethod, http.MethodDelete; got != want {
		t.Fatalf("Delete method = %q, want %q", got, want)
	}
	if got, want := operations["Delete"].ResolvedSuccessStatus(), http.StatusNoContent; got != want {
		t.Fatalf("Delete status = %d, want %d", got, want)
	}
	if got, want := operations["Replace"].HTTPMethod, http.MethodPut; got != want {
		t.Fatalf("Replace method = %q, want %q", got, want)
	}
	if got, want := operations["Update"].HTTPMethod, http.MethodPatch; got != want {
		t.Fatalf("Update method = %q, want %q", got, want)
	}

	created, err := compiled.Invokers["Create"](fb.Invocation{
		Context: context.Background(),
		DecodeBody: func(target any) error {
			request := target.(*domain.Request)
			request.Name = "fixture-1"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Create invoker returned an error: %v", err)
	}
	if response, ok := created.Value.(domain.Response); !ok || response.ID != "fixture-1" || !created.HasValue {
		t.Fatalf("Create result = %#v, want domain.Response", created)
	}

	nilResult, err := compiled.Invokers["Nil"](fb.Invocation{Context: context.Background()})
	if err != nil || !nilResult.HasValue || nilResult.Value != nil {
		t.Fatalf("Nil result = %#v, error = %v; want an intentional nil value", nilResult, err)
	}
	deleted, err := compiled.Invokers["Delete"](fb.Invocation{Context: context.Background()})
	if err != nil || deleted.HasValue {
		t.Fatalf("Delete result = %#v, error = %v; want void result", deleted, err)
	}
}

func TestStaticFixtureHTTPParity(t *testing.T) {
	api := &staticfixture.API{}
	app := fb.MakeRawApp()
	app.AddApi(api)
	router := app.(fb.WithRouter).Router()

	created := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/fixtures", strings.NewReader(`{"name":"fixture-http"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(created, createRequest)
	if got, want := created.Code, http.StatusCreated; got != want {
		t.Fatalf("POST status = %d, want %d: %s", got, want, created.Body.String())
	}
	var response domain.Response
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil || response.ID != "fixture-http" {
		t.Fatalf("POST response = %#v, error = %v", response, err)
	}

	nilResponse := httptest.NewRecorder()
	router.ServeHTTP(nilResponse, httptest.NewRequest(http.MethodGet, "/fixtures/nil", nil))
	if got, want := nilResponse.Code, http.StatusOK; got != want || strings.TrimSpace(nilResponse.Body.String()) != "null" {
		t.Fatalf("GET nil status/body = %d/%q, want 200/null", got, nilResponse.Body.String())
	}

	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/fixtures/fixture-http", nil))
	if got, want := deleted.Code, http.StatusNoContent; got != want || deleted.Body.Len() != 0 {
		t.Fatalf("DELETE status/body = %d/%q, want 204/empty", got, deleted.Body.String())
	}
	if got, want := api.DeletedID, "fixture-http"; got != want {
		t.Fatalf("Delete path parameter = %q, want %q", got, want)
	}

	replaced := httptest.NewRecorder()
	replaceRequest := httptest.NewRequest(http.MethodPut, "/fixtures/ids", strings.NewReader(`["one","two"]`))
	replaceRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(replaced, replaceRequest)
	if got, want := replaced.Code, http.StatusOK; got != want || strings.TrimSpace(replaced.Body.String()) != `["one","two"]` {
		t.Fatalf("PUT named collection status/body = %d/%q, want 200/[\"one\",\"two\"]", got, replaced.Body.String())
	}

	updated := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPatch, "/fixtures/fixture-http", strings.NewReader(`{"name":"updated"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(updated, updateRequest)
	if got, want := updated.Code, http.StatusOK; got != want || strings.TrimSpace(updated.Body.String()) != `{"id":"updated"}` {
		t.Fatalf("PATCH status/body = %d/%q, want 200/update response", got, updated.Body.String())
	}
}
