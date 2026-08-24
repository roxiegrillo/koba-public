package koba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	lerror "github.com/roxiegrillo/koba/v2/errors"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
)

func TestGetTextAndStatusCode(t *testing.T) {
	errorStr, code := getErrorMessageAndStatusCode(500)

	if errorStr != "unknown error: 500" || code != 500 {
		t.Error("shoudld error: Str!=\"unknown error: 500\" || code != 500")
	}

	errorStr, code = getErrorMessageAndStatusCode(errors.New("my error"))

	if errorStr != "my error" || code != 500 {
		t.Error("shoudld error: errorStr!= \"my error\" || code != 500")
	}

	errorStr, code = getErrorMessageAndStatusCode(lerror.MakeError(200, errors.New("dummy error")))

	if errorStr != "dummy error" || code != 200 {
		t.Error("shoudld error: errorStr!= \"dummy error\" || code != 200")
	}
}

type pathParameterFixtureAPI struct{}

func (*pathParameterFixtureAPI) Get(ctx context.Context) map[string]string {
	return http_common.PathParametersFromContext(ctx)
}

func (*pathParameterFixtureAPI) Routes() map[string]string {
	return map[string]string{"Get": "/items/:id"}
}

func TestAPIMethodCanReadNamedPathParameters(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&pathParameterFixtureAPI{})
	router := app.(WithRouter).Router()

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/item-123", nil))
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}
	var parameters map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &parameters); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := parameters["id"], "item-123"; got != want {
		t.Fatalf("path parameter id = %q, want %q", got, want)
	}
}
