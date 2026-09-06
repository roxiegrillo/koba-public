package koba_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	fb "github.com/roxiegrillo/koba/v2"
	fberrors "github.com/roxiegrillo/koba/v2/errors"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
)

func TestStaticFixtureHTTPParity(t *testing.T) {
	api := &staticfixture.API{}
	app := newStaticFixtureApp()
	app.AddApi(api)
	router := app.(fb.WithRouter).Router()

	server := httptest.NewServer(router)
	defer server.Close()
	connection, upgradeResponse, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/fixtures/connect",
		nil,
	)
	if err != nil {
		t.Fatalf("dial fixture WebSocket: %v (response %#v)", err, upgradeResponse)
	}
	defer connection.Close()
	if err := connection.WriteJSON(domain.WebSocketInput{Message: "hello"}); err != nil {
		t.Fatalf("write fixture WebSocket message: %v", err)
	}
	var socketResponse domain.WebSocketOutput
	if err := connection.ReadJSON(&socketResponse); err != nil {
		t.Fatalf("read fixture WebSocket response: %v", err)
	}
	if socketResponse.ID != "fixture" || socketResponse.Message != "hello" {
		t.Fatalf("fixture WebSocket response = %#v", socketResponse)
	}

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
	api.CreateError = fberrors.MakeError(http.StatusConflict, "fixture already exists")
	failedCreate := httptest.NewRecorder()
	failedCreateRequest := httptest.NewRequest(http.MethodPost, "/fixtures", strings.NewReader(`{"name":"fixture-conflict"}`))
	failedCreateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(failedCreate, failedCreateRequest)
	if got, want := failedCreate.Code, http.StatusConflict; got != want || !strings.Contains(failedCreate.Body.String(), "fixture already exists") || strings.Contains(failedCreate.Body.String(), "fixture-conflict") {
		t.Fatalf("failed POST status/body = %d/%q, want 409 error without response value", got, failedCreate.Body.String())
	}
	api.CreateError = nil

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
	api.DeleteError = fberrors.MakeNotFoundError("fixture missing")
	failedDelete := httptest.NewRecorder()
	router.ServeHTTP(failedDelete, httptest.NewRequest(http.MethodDelete, "/fixtures/missing", nil))
	if got, want := failedDelete.Code, http.StatusNotFound; got != want || !strings.Contains(failedDelete.Body.String(), "fixture missing") {
		t.Fatalf("failed DELETE status/body = %d/%q, want 404 operation error", got, failedDelete.Body.String())
	}
	api.DeleteError = nil

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

	invalidPair := httptest.NewRecorder()
	invalidPairRequest := httptest.NewRequest(
		http.MethodPatch,
		"/fixtures/fixture-http/compose",
		strings.NewReader(`{"name":"partial coordinates","details":{"note":"invalid"},"lat":19.4}`),
	)
	invalidPairRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalidPair, invalidPairRequest)
	if got, want := invalidPair.Code, http.StatusBadRequest; got != want || !strings.Contains(invalidPair.Body.String(), "lat, lon must be provided together") {
		t.Fatalf("partial coordinate pair = %d %s, want 400 all-or-none error", got, invalidPair.Body.String())
	}
	if api.ComposeCount != 1 {
		t.Fatalf("invalid coordinate pair invoked Compose; count = %d", api.ComposeCount)
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

func TestStructuralBodyLimitErrorParity(t *testing.T) {
	api := &staticfixture.API{}
	app := newStaticFixtureApp()
	app.(fb.WithMiddlewareUse).Use(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 8)
	}))
	app.AddApi(api)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/fixtures",
		strings.NewReader(`{"name":"too large"}`),
	)
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	request.Header.Set("Content-Type", "application/json")
	app.(http.Handler).ServeHTTP(response, request)

	if got, want := response.Code, http.StatusRequestEntityTooLarge; got != want {
		t.Fatalf("streaming POST status = %d, want %d: %s", got, want, response.Body.String())
	}
	var body struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != http.StatusRequestEntityTooLarge || body.Error != "request_entity_too_large" {
		t.Fatalf("body = %+v, want 413 request_entity_too_large", body)
	}
	if api.CreateCount != 0 {
		t.Fatalf("oversized structural body invoked Create; count = %d", api.CreateCount)
	}
}
