package koba

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type semanticsResponse struct {
	Method string `json:"method"`
}

type semanticsAPI struct{}

func (*semanticsAPI) Void() {}

func (*semanticsAPI) Accepted() {}

func (*semanticsAPI) Reset() {}

func (*semanticsAPI) Nil() *semanticsResponse { return nil }

func (*semanticsAPI) Replace(_ overrideFixtureRequest) semanticsResponse {
	return semanticsResponse{Method: http.MethodPut}
}

func (*semanticsAPI) Delete() {}

func (*semanticsAPI) Routes() map[string]string {
	return map[string]string{
		"Void":     "/semantics/void",
		"Accepted": "/semantics/accepted",
		"Reset":    "/semantics/reset",
		"Nil":      "/semantics/nil",
		"Replace":  "/semantics/:id",
		"Delete":   "/semantics/:id",
	}
}

func (*semanticsAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"Accepted": {SuccessStatus: http.StatusAccepted},
		"Reset":    {SuccessStatus: http.StatusResetContent},
		"Replace":  {Method: http.MethodPut},
		"Delete":   {Method: http.MethodDelete},
	}}
}

func TestRuntimeSuccessAndMethodSemantics(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&semanticsAPI{})
	router := app.(WithRouter).Router()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{name: "void defaults to 204", method: http.MethodGet, path: "/semantics/void", wantStatus: http.StatusNoContent},
		{name: "void honors 202 override", method: http.MethodGet, path: "/semantics/accepted", wantStatus: http.StatusAccepted},
		{name: "205 never emits a body", method: http.MethodGet, path: "/semantics/reset", wantStatus: http.StatusResetContent},
		{name: "nil pointer is JSON null", method: http.MethodGet, path: "/semantics/nil", wantStatus: http.StatusOK, wantBody: "null"},
		{name: "PUT is routed", method: http.MethodPut, path: "/semantics/item-1", body: `{"name":"replacement"}`, wantStatus: http.StatusOK, wantBody: `{"method":"PUT"}`},
		{name: "DELETE defaults to 204", method: http.MethodDelete, path: "/semantics/item-1", wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if got := strings.TrimSpace(response.Body.String()); got != test.wantBody {
				t.Fatalf("body = %q, want %q", got, test.wantBody)
			}
		})
	}
}
