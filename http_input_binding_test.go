package koba

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

type textArrayScalar [16]byte

func (*textArrayScalar) UnmarshalText([]byte) error { return nil }

type namedTextScalar string

func (value *namedTextScalar) UnmarshalText(text []byte) error {
	if string(text) != "valid" {
		return fmt.Errorf("want valid")
	}
	*value = "decoded"
	return nil
}

type structuralBindingAPI struct {
	calls            int
	identifier       string
	pageSize         int
	pageSizeProvided bool
	label            string
	traceID          string
	sessionID        string
}

func (api *structuralBindingAPI) Read(ctx context.Context) map[string]bool {
	api.calls++
	api.identifier, _ = InputValue[string](ctx, "identifier")
	api.pageSize, _ = InputValue[int](ctx, "pageSize")
	api.pageSizeProvided = InputWasProvided(ctx, "pageSize")
	api.label, _ = InputValue[string](ctx, "label")
	api.traceID, _ = InputValue[string](ctx, "traceID")
	api.sessionID, _ = InputValue[string](ctx, "sessionID")
	return map[string]bool{"ok": true}
}

func (*structuralBindingAPI) Routes() map[string]string {
	return map[string]string{"Read": "/structural/:id"}
}

func (*structuralBindingAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{"Read": {Parameters: []ParameterDocumentation{
		{Name: "id", In: ParameterInPath, ContractInput: "identifier"},
		{Name: "page_size", In: ParameterInQuery, ContractInput: "pageSize"},
		{Name: "label", In: ParameterInQuery, ContractInput: "label"},
		{Name: "X-Trace-ID", In: ParameterInHeader, ContractInput: "traceID"},
		{Name: "session", In: ParameterInCookie, ContractInput: "sessionID"},
	}}}}
}

func (*structuralBindingAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{"Read": {Inputs: map[string]InputContract{
		"identifier": InputOf[string](Required(), MinLength(1)),
		"pageSize":   InputOf[int](Default(20), Min(1), Max(100)),
		"label":      InputOf[string](MaxLength(2)),
		"traceID":    InputOf[string](Required(), MinLength(1), MaxLength(16)),
		"sessionID":  InputOf[string](MaxLength(16)),
	}}}}
}

func TestHTTPStructuralBindingAppliesDefaultsAndAllParameterLocations(t *testing.T) {
	api := &structuralBindingAPI{}
	app := MakeRawApp()
	app.AddApi(api)

	request := httptest.NewRequest(http.MethodGet, "/structural/item-1?label="+url.QueryEscape("é😊"), nil)
	request.Header.Set("X-Trace-ID", "trace-1")
	request.AddCookie(&http.Cookie{Name: "session", Value: "session-1"})
	response := httptest.NewRecorder()
	app.(WithRouter).Router().ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}
	if api.calls != 1 || api.identifier != "item-1" || api.pageSize != 20 || api.pageSizeProvided || api.label != "é😊" || api.traceID != "trace-1" || api.sessionID != "session-1" {
		t.Fatalf("bound values = calls=%d id=%q page=%d provided=%v label=%q trace=%q session=%q", api.calls, api.identifier, api.pageSize, api.pageSizeProvided, api.label, api.traceID, api.sessionID)
	}
}

func TestHTTPStructuralBindingRejectsInvalidOrAmbiguousScalarsBeforeInvocation(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		prepare func(*http.Request)
	}{
		{name: "missing required header", target: "/structural/item-1"},
		{name: "empty required header", target: "/structural/item-1", prepare: func(request *http.Request) { request.Header["X-Trace-Id"] = []string{""} }},
		{name: "numeric below minimum", target: "/structural/item-1?page_size=0", prepare: addTraceHeader},
		{name: "numeric above maximum", target: "/structural/item-1?page_size=101", prepare: addTraceHeader},
		{name: "invalid numeric encoding", target: "/structural/item-1?page_size=many", prepare: addTraceHeader},
		{name: "repeated query scalar", target: "/structural/item-1?page_size=1&page_size=2", prepare: addTraceHeader},
		{name: "malformed query", target: "/structural/item-1", prepare: func(request *http.Request) {
			addTraceHeader(request)
			request.URL.RawQuery = "page_size=%zz"
		}},
		{name: "string exceeds Unicode max length", target: "/structural/item-1?label=" + url.QueryEscape("é😊x"), prepare: addTraceHeader},
		{name: "repeated header scalar", target: "/structural/item-1", prepare: func(request *http.Request) {
			request.Header.Add("X-Trace-ID", "one")
			request.Header.Add("X-Trace-ID", "two")
		}},
		{name: "repeated cookie scalar", target: "/structural/item-1", prepare: func(request *http.Request) {
			addTraceHeader(request)
			request.AddCookie(&http.Cookie{Name: "session", Value: "one"})
			request.AddCookie(&http.Cookie{Name: "session", Value: "two"})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &structuralBindingAPI{}
			app := MakeRawApp()
			app.AddApi(api)
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.prepare != nil {
				test.prepare(request)
			}
			response := httptest.NewRecorder()
			app.(WithRouter).Router().ServeHTTP(response, request)
			if got, want := response.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
			}
			if api.calls != 0 {
				t.Fatalf("API invoked %d times for invalid input", api.calls)
			}
		})
	}
}

func TestHTTPScalarTypesRejectStructuredTextUnmarshalers(t *testing.T) {
	if err := validateHTTPScalarType(reflect.TypeFor[textArrayScalar]()); err == nil || !strings.Contains(err.Error(), "does not support HTTP scalar decoding") {
		t.Fatalf("validateHTTPScalarType(textArrayScalar) error = %v, want unsupported scalar", err)
	}
	if _, err := coerceHTTPScalar("anything", reflect.TypeFor[textArrayScalar]()); err == nil || !strings.Contains(err.Error(), "does not support HTTP scalar decoding") {
		t.Fatalf("coerceHTTPScalar(textArrayScalar) error = %v, want unsupported scalar", err)
	}
}

func TestHTTPScalarTypesSupportNamedPrimitiveTextUnmarshalers(t *testing.T) {
	if err := validateHTTPScalarType(reflect.TypeFor[namedTextScalar]()); err != nil {
		t.Fatalf("validateHTTPScalarType(namedTextScalar) returned an error: %v", err)
	}
	value, err := coerceHTTPScalar("valid", reflect.TypeFor[namedTextScalar]())
	if err != nil {
		t.Fatalf("coerceHTTPScalar(namedTextScalar) returned an error: %v", err)
	}
	if got, want := value.(namedTextScalar), namedTextScalar("decoded"); got != want {
		t.Fatalf("coerced value = %q, want %q", got, want)
	}
	if _, err := coerceHTTPScalar("invalid", reflect.TypeFor[namedTextScalar]()); err == nil || !strings.Contains(err.Error(), "want valid") {
		t.Fatalf("invalid named scalar error = %v, want unmarshaler error", err)
	}
}

func addTraceHeader(request *http.Request) {
	request.Header.Set("X-Trace-ID", "trace-1")
}

func TestAuthorizationRunsBeforeStructuralValidation(t *testing.T) {
	invoked := false
	operation := OperationDescription{
		MethodName: "Protected",
		Route:      "/protected",
		HTTPMethod: http.MethodGet,
		Contract: OperationContract{Inputs: map[string]InputContract{
			"traceID": InputOf[string](Required()),
		}},
		Documentation: OperationDocumentation{
			RequiredScopes: []string{"read:protected"},
			Parameters: []ParameterDocumentation{{
				Name: "X-Trace-ID", In: ParameterInHeader, ContractInput: "traceID",
			}},
		},
	}
	handler := makeOperationHandler(operation, func(Invocation) (InvocationResult, error) {
		invoked = true
		return InvocationResult{}, nil
	})
	router := httprouter.New()
	router.GET("/protected", handler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if got, want := response.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}
	if invoked || strings.Contains(response.Body.String(), "traceID") {
		t.Fatalf("structural validation ran before authorization: invoked=%v body=%s", invoked, response.Body.String())
	}
}

type legacyParameterAPI struct{ calls int }

func (api *legacyParameterAPI) Read() { api.calls++ }

func (*legacyParameterAPI) Routes() map[string]string {
	return map[string]string{"Read": "/legacy-parameter"}
}

func (*legacyParameterAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{"Read": {Parameters: []ParameterDocumentation{
		ParameterOf[int]("page", ParameterInQuery, "Legacy page", false),
	}}}}
}

func TestLegacyParameterDocumentationRemainsDocumentationOnly(t *testing.T) {
	api := &legacyParameterAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	response := httptest.NewRecorder()
	app.(WithRouter).Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/legacy-parameter?page=not-an-integer", nil))
	if response.Code != http.StatusNoContent || api.calls != 1 {
		t.Fatalf("legacy parameter response/calls = %d/%d, want 204/1: %s", response.Code, api.calls, response.Body.String())
	}
}
