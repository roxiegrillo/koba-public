package koba

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	fberrors "github.com/roxiegrillo/koba/v2/errors"
)

type operationErrorRequest struct {
	Name string `json:"name"`
}

type operationErrorResponse struct {
	Message string `json:"message"`
}

type operationErrorAPI struct {
	failure    error
	panicValue any
	response   operationErrorResponse
}

type errorBeforeResponseAPI struct{}

func (*errorBeforeResponseAPI) Broken() (error, operationErrorResponse) {
	return nil, operationErrorResponse{}
}

type nonErrorSecondResultAPI struct{}

func (*nonErrorSecondResultAPI) Broken() (operationErrorResponse, string) {
	return operationErrorResponse{}, ""
}

type tooManyResultsAPI struct{}

func (*tooManyResultsAPI) Broken() (operationErrorResponse, error, error) {
	return operationErrorResponse{}, nil, nil
}

func (api *operationErrorAPI) ErrorOnly() error {
	return api.failure
}

func (api *operationErrorAPI) Value() (operationErrorResponse, error) {
	return api.response, api.failure
}

func (api *operationErrorAPI) Create(request operationErrorRequest) (operationErrorResponse, error) {
	return operationErrorResponse{Message: request.Name}, api.failure
}

func (api *operationErrorAPI) Validate(_ operationErrorRequest) error {
	return api.failure
}

func (api *operationErrorAPI) Panic() error {
	panic(api.panicValue)
}

func (*operationErrorAPI) Routes() map[string]string {
	return map[string]string{
		"Create":    "/operation-errors",
		"ErrorOnly": "/operation-errors/void",
		"Panic":     "/operation-errors/panic",
		"Validate":  "/operation-errors/validate",
		"Value":     "/operation-errors/value",
	}
}

func TestOptionalOperationErrorDescriptionAndInvocation(t *testing.T) {
	description, err := DescribeReflectedAPI((*operationErrorAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	operations := make(map[string]OperationDescription, len(description.Operations))
	for _, operation := range description.Operations {
		operations[operation.MethodName] = operation
	}

	if got := operations["ErrorOnly"].OutputType; got != nil {
		t.Fatalf("ErrorOnly OutputType = %v, want nil", got)
	}
	if got, want := operations["ErrorOnly"].ResolvedSuccessStatus(), http.StatusNoContent; got != want {
		t.Fatalf("ErrorOnly success status = %d, want %d", got, want)
	}
	for _, method := range []string{"Create", "Value"} {
		if got, want := operations[method].OutputType, reflect.TypeFor[operationErrorResponse](); got != want {
			t.Fatalf("%s OutputType = %v, want %v", method, got, want)
		}
		if got, want := operations[method].ResolvedSuccessStatus(), http.StatusOK; got != want {
			t.Fatalf("%s success status = %d, want %d", method, got, want)
		}
	}
	if got, want := operations["Create"].InputType, reflect.TypeFor[operationErrorRequest](); got != want {
		t.Fatalf("Create InputType = %v, want %v", got, want)
	}
	if got, want := operations["Validate"].InputType, reflect.TypeFor[operationErrorRequest](); got != want {
		t.Fatalf("Validate InputType = %v, want %v", got, want)
	}
	if got := operations["Validate"].OutputType; got != nil {
		t.Fatalf("Validate OutputType = %v, want nil", got)
	}
	if got, want := operations["Validate"].HTTPMethod, http.MethodPost; got != want {
		t.Fatalf("Validate HTTP method = %q, want %q", got, want)
	}

	api := &operationErrorAPI{response: operationErrorResponse{Message: "ok"}}
	compiled, err := CompileAPI(api)
	if err != nil {
		t.Fatalf("CompileAPI() returned an error: %v", err)
	}

	voidResult, err := compiled.Invokers["ErrorOnly"](Invocation{})
	if err != nil || voidResult.HasValue {
		t.Fatalf("ErrorOnly result = %#v, error = %v; want void success", voidResult, err)
	}
	valueResult, err := compiled.Invokers["Value"](Invocation{})
	if err != nil {
		t.Fatalf("Value invoker returned an error: %v", err)
	}
	if got, ok := valueResult.Value.(operationErrorResponse); !ok || !valueResult.HasValue || got.Message != "ok" {
		t.Fatalf("Value result = %#v, want successful response", valueResult)
	}

	wantFailure := errors.New("operation failed")
	api.failure = wantFailure
	failedResult, err := compiled.Invokers["Value"](Invocation{})
	if !errors.Is(err, wantFailure) {
		t.Fatalf("Value invoker error = %v, want %v", err, wantFailure)
	}
	if failedResult.HasValue || failedResult.Value != nil {
		t.Fatalf("Value result on error = %#v, want discarded response", failedResult)
	}
}

func TestOptionalOperationErrorRejectsInvalidResultSignatures(t *testing.T) {
	tests := []struct {
		name string
		api  any
		want string
	}{
		{
			name: "error before response",
			api:  (*errorBeforeResponseAPI)(nil),
			want: "response value cannot be error",
		},
		{
			name: "second result is not error",
			api:  (*nonErrorSecondResultAPI)(nil),
			want: "second result must be error",
		},
		{
			name: "more than response and error",
			api:  (*tooManyResultsAPI)(nil),
			want: "response value followed by an optional error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeReflectedAPI(test.api)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DescribeReflectedAPI() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestOptionalOperationErrorHTTPBehavior(t *testing.T) {
	t.Run("error-only nil succeeds without a response", func(t *testing.T) {
		response := serveOperationErrorRequest(t, &operationErrorAPI{}, http.MethodGet, "/operation-errors/void", "")
		if got, want := response.Code, http.StatusNoContent; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
	})

	t.Run("value and nil error return the response", func(t *testing.T) {
		api := &operationErrorAPI{response: operationErrorResponse{Message: "ok"}}
		response := serveOperationErrorRequest(t, api, http.MethodGet, "/operation-errors/value", "")
		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
		if got := response.Body.String(); !strings.Contains(got, `"message":"ok"`) {
			t.Fatalf("response body = %q, want successful value", got)
		}
	})

	t.Run("request and nil error succeeds without a response", func(t *testing.T) {
		response := serveOperationErrorRequest(
			t,
			&operationErrorAPI{},
			http.MethodPost,
			"/operation-errors/validate",
			`{"name":"valid"}`,
		)
		if got, want := response.Code, http.StatusNoContent; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
	})

	t.Run("request and error uses operation error mapping", func(t *testing.T) {
		response := serveOperationErrorRequest(
			t,
			&operationErrorAPI{failure: fberrors.MakeError(http.StatusConflict, "request conflicts")},
			http.MethodPost,
			"/operation-errors/validate",
			`{"name":"valid"}`,
		)
		assertOperationErrorResponse(t, response, http.StatusConflict, "request conflicts")
	})

	t.Run("status-coded error returns its HTTP status", func(t *testing.T) {
		api := &operationErrorAPI{failure: fberrors.MakeNotFoundError("item missing")}
		response := serveOperationErrorRequest(t, api, http.MethodGet, "/operation-errors/void", "")
		assertOperationErrorResponse(t, response, http.StatusNotFound, "item missing")
	})

	t.Run("wrapped status-coded error preserves its HTTP status", func(t *testing.T) {
		api := &operationErrorAPI{
			failure:  fmt.Errorf("lookup item: %w", fberrors.MakeNotFoundError("item missing")),
			response: operationErrorResponse{Message: "must-not-leak"},
		}
		response := serveOperationErrorRequest(t, api, http.MethodGet, "/operation-errors/value", "")
		assertOperationErrorResponse(t, response, http.StatusNotFound, "lookup item: item missing")
		if strings.Contains(response.Body.String(), "must-not-leak") {
			t.Fatalf("response body leaked the successful return value: %s", response.Body.String())
		}
	})

	t.Run("plain error becomes internal server error and discards response", func(t *testing.T) {
		api := &operationErrorAPI{
			failure:  errors.New("storage unavailable"),
			response: operationErrorResponse{Message: "must-not-leak"},
		}
		response := serveOperationErrorRequest(t, api, http.MethodGet, "/operation-errors/value", "")
		assertOperationErrorResponse(t, response, http.StatusInternalServerError, "storage unavailable")
		if strings.Contains(response.Body.String(), "must-not-leak") {
			t.Fatalf("response body leaked the successful return value: %s", response.Body.String())
		}
	})

	t.Run("malformed JSON remains a bad request", func(t *testing.T) {
		response := serveOperationErrorRequest(t, &operationErrorAPI{}, http.MethodPost, "/operation-errors", `{"name":`)
		if got, want := response.Code, http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
		}
	})

	t.Run("panic compatibility is retained", func(t *testing.T) {
		api := &operationErrorAPI{panicValue: fberrors.MakeNotFoundError("panic item missing")}
		response := serveOperationErrorRequest(t, api, http.MethodGet, "/operation-errors/panic", "")
		assertOperationErrorResponse(t, response, http.StatusNotFound, "panic item missing")
	})
}

func serveOperationErrorRequest(
	t *testing.T,
	api *operationErrorAPI,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	app := MakeRawApp()
	app.AddApi(api)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	app.(WithRouter).Router().ServeHTTP(response, request)
	return response
}

func assertOperationErrorResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantMessage string,
) {
	t.Helper()
	if got := response.Code; got != wantStatus {
		t.Fatalf("status = %d, want %d: %s", got, wantStatus, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, fmt.Sprintf(`"status":%d`, wantStatus)) ||
		!strings.Contains(got, fmt.Sprintf(`"message":%q`, wantMessage)) {
		t.Fatalf("response body = %q, want status %d and message %q", got, wantStatus, wantMessage)
	}
}
