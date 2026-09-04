package koba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type standardHTTPHandlerFixture struct{}

type standardHTTPBodyFixture struct {
	Value string `json:"value"`
}

func (*standardHTTPHandlerFixture) Routes() map[string]string {
	return map[string]string{
		"Create":   "/body",
		"Greeting": "/greeting",
	}
}

func (*standardHTTPHandlerFixture) Greeting(context.Context) struct {
	Message string `json:"message"`
} {
	return struct {
		Message string `json:"message"`
	}{Message: "hello"}
}

func (*standardHTTPHandlerFixture) Create(_ standardHTTPBodyFixture) struct{} {
	return struct{}{}
}

func TestKobaAppImplementsStandardHTTPHandler(t *testing.T) {
	app := MakeDefaultKobaApp()
	app.AddApi(&standardHTTPHandlerFixture{})

	handler, ok := app.(http.Handler)
	if !ok {
		t.Fatal("Koba app does not implement http.Handler")
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/greeting", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Message != "hello" {
		t.Fatalf("message = %q, want hello", body.Message)
	}
}

func TestKobaAppOwnsBodyDecodeErrorResponse(t *testing.T) {
	app := MakeRawApp()
	app.(WithMiddlewareUse).Use(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 8)
	}))
	app.AddApi(&standardHTTPHandlerFixture{})
	handler := app.(http.Handler)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/body",
		strings.NewReader(`{"value":"too large"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
	var body struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != http.StatusRequestEntityTooLarge || body.Error != "request_entity_too_large" {
		t.Fatalf("body = %+v", body)
	}
}
