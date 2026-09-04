package koba_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
)

func TestStaticFixtureHTTPParity(t *testing.T) {
	api := &staticfixture.API{}
	app := newStaticFixtureApp()
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

	inspected := httptest.NewRecorder()
	inspectRequest := httptest.NewRequest(http.MethodGet, "/fixtures/fixture-http/inspect", nil)
	inspectRequest.Header.Set("X-Trace-ID", "trace-1")
	inspectRequest.AddCookie(&http.Cookie{Name: "session", Value: "session-1"})
	router.ServeHTTP(inspected, inspectRequest)
	if got, want := inspected.Code, http.StatusOK; got != want {
		t.Fatalf("inspect status = %d, want %d: %s", got, want, inspected.Body.String())
	}
	if api.InspectCount != 1 || api.InspectedID != "fixture-http" || api.PageSize != 20 || api.PageSizeProvided || api.TraceID != "trace-1" || api.SessionID != "session-1" {
		t.Fatalf("bound inputs = count=%d id=%q page=%d provided=%v trace=%q session=%q", api.InspectCount, api.InspectedID, api.PageSize, api.PageSizeProvided, api.TraceID, api.SessionID)
	}

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodGet, "/fixtures/fixture-http/inspect?page_size=101", nil)
	invalidRequest.Header.Set("X-Trace-ID", "trace-2")
	router.ServeHTTP(invalid, invalidRequest)
	if got, want := invalid.Code, http.StatusBadRequest; got != want {
		t.Fatalf("invalid input status = %d, want %d: %s", got, want, invalid.Body.String())
	}
	if api.InspectCount != 1 {
		t.Fatalf("invalid structural input invoked API; count = %d", api.InspectCount)
	}

	composed := httptest.NewRecorder()
	composeRequest := httptest.NewRequest(
		http.MethodPatch,
		"/fixtures/fixture-http/compose?page_size=25",
		strings.NewReader(`{"name":"composed","details":{"note":"deep value"}}`),
	)
	composeRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(composed, composeRequest)
	if got, want := composed.Code, http.StatusOK; got != want {
		t.Fatalf("compose status = %d, want %d: %s", got, want, composed.Body.String())
	}
	if api.ComposeCount != 1 || api.Composed.ID != "fixture-http" || api.Composed.PageSize == nil || *api.Composed.PageSize != 25 || api.Composed.Payload.Name != "composed" || api.Composed.Payload.Details.Note != "deep value" {
		t.Fatalf("composed request = count=%d request=%#v", api.ComposeCount, api.Composed)
	}

	invalidComposition := httptest.NewRecorder()
	invalidComposeRequest := httptest.NewRequest(
		http.MethodPatch,
		"/fixtures/fixture-http/compose",
		strings.NewReader(`{"name":"missing details"}`),
	)
	invalidComposeRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalidComposition, invalidComposeRequest)
	if got, want := invalidComposition.Code, http.StatusBadRequest; got != want {
		t.Fatalf("invalid compose status = %d, want %d: %s", got, want, invalidComposition.Body.String())
	}
	if api.ComposeCount != 1 {
		t.Fatalf("invalid deep body invoked Compose; count = %d", api.ComposeCount)
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
