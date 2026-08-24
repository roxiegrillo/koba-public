package koba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type compiledPlanRequest struct {
	Name string `json:"name" binding:"required"`
}

type compiledPlanResponse struct {
	Source string `json:"source"`
	Name   string `json:"name"`
}

type compiledPlanFixtureAPI struct{}

// Create deliberately differs from the generated invoker so the HTTP test can
// prove AddApi selected the compiled plan instead of reflection.
func (*compiledPlanFixtureAPI) Create(context.Context, compiledPlanRequest) compiledPlanResponse {
	return compiledPlanResponse{Source: "reflection"}
}

func (api *compiledPlanFixtureAPI) KobaCompiledAPI() CompiledAPI {
	return CompiledAPI{
		Description: APIDescription{
			Name: "compiled-plan",
			Operations: []OperationDescription{
				{
					MethodName:  "Create",
					OperationID: "compiledPlanCreate",
					Route:       "/compiled-plan",
					HTTPMethod:  http.MethodPost,
					InputType:   reflect.TypeFor[compiledPlanRequest](),
					OutputType:  reflect.TypeFor[compiledPlanResponse](),
				},
			},
		},
		Invokers: map[string]OperationInvoker{
			"Create": func(invocation Invocation) (InvocationResult, error) {
				var request compiledPlanRequest
				if err := invocation.Decode(&request); err != nil {
					return InvocationResult{}, err
				}
				return InvocationResult{
					Value:    compiledPlanResponse{Source: "generated", Name: request.Name},
					HasValue: true,
				}, nil
			},
		},
	}
}

func TestInvocationDecodeRequiresTransportDecoder(t *testing.T) {
	err := (Invocation{}).Decode(&compiledPlanRequest{})
	if err == nil || !strings.Contains(err.Error(), "body decoder is not available") {
		t.Fatalf("Invocation.Decode() error = %v, want missing decoder error", err)
	}
}

func TestCompileAPIPrefersGeneratedPlan(t *testing.T) {
	compiled, err := CompileAPI((*compiledPlanFixtureAPI)(nil))
	if err != nil {
		t.Fatalf("CompileAPI() returned an error: %v", err)
	}
	if got, want := compiled.Description.Operations[0].Route, "/compiled-plan"; got != want {
		t.Fatalf("compiled route = %q, want %q", got, want)
	}

	app := MakeRawApp()
	app.AddApi(&compiledPlanFixtureAPI{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/compiled-plan", strings.NewReader(`{"name":"widget"}`))
	request.Header.Set("Content-Type", "application/json")
	app.(WithRouter).Router().ServeHTTP(response, request)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}
	var body compiledPlanResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := body.Source, "generated"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := body.Name, "widget"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestCompiledPlanUsesSharedBadRequestHandling(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&compiledPlanFixtureAPI{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/compiled-plan", strings.NewReader(`{"name":`))
	request.Header.Set("Content-Type", "application/json")
	app.(WithRouter).Router().ServeHTTP(response, request)
	if got, want := response.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}
}

type invalidCompiledPlanAPI struct {
	compiled CompiledAPI
}

func (api invalidCompiledPlanAPI) KobaCompiledAPI() CompiledAPI {
	return api.compiled
}

func TestCompileAPIValidatesGeneratedPlan(t *testing.T) {
	tests := []struct {
		name     string
		compiled CompiledAPI
		want     string
	}{
		{name: "no operations", compiled: CompiledAPI{}, want: "contains no operations"},
		{
			name: "missing invoker",
			compiled: CompiledAPI{Description: APIDescription{Operations: []OperationDescription{
				{MethodName: "Get"},
			}}},
			want: `no invoker for "Get"`,
		},
		{
			name: "unknown invoker",
			compiled: CompiledAPI{
				Description: APIDescription{Operations: []OperationDescription{{MethodName: "Get"}}},
				Invokers: map[string]OperationInvoker{
					"Get":    func(Invocation) (InvocationResult, error) { return InvocationResult{}, nil },
					"Create": func(Invocation) (InvocationResult, error) { return InvocationResult{}, nil },
				},
			},
			want: `unknown method "Create"`,
		},
		{
			name: "non-2xx success status",
			compiled: CompiledAPI{
				Description: APIDescription{Operations: []OperationDescription{{
					MethodName:  "Get",
					OperationID: "invalidStatusGet",
					Route:       "/invalid-status",
					HTTPMethod:  http.MethodGet,
					Documentation: OperationDocumentation{
						SuccessStatus: 300,
					},
				}}},
				Invokers: map[string]OperationInvoker{
					"Get": func(Invocation) (InvocationResult, error) { return InvocationResult{}, nil },
				},
			},
			want: "between 200 and 299",
		},
		{
			name: "204 with response value",
			compiled: CompiledAPI{
				Description: APIDescription{Operations: []OperationDescription{{
					MethodName:    "Get",
					OperationID:   "invalidNoContentGet",
					Route:         "/invalid-no-content",
					HTTPMethod:    http.MethodGet,
					OutputType:    reflect.TypeFor[compiledPlanResponse](),
					Documentation: OperationDocumentation{SuccessStatus: http.StatusNoContent},
				}}},
				Invokers: map[string]OperationInvoker{
					"Get": func(Invocation) (InvocationResult, error) { return InvocationResult{}, nil },
				},
			},
			want: "cannot be used by an operation with a response value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileAPI(invalidCompiledPlanAPI{compiled: test.compiled})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CompileAPI() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

type generatedProviderBusinessMethodAPI struct{}

func (generatedProviderBusinessMethodAPI) KobaCompiledAPI(context.Context) string {
	return "business response"
}

func TestBusinessMethodNamedGeneratedProviderRemainsEndpoint(t *testing.T) {
	description, err := DescribeAPI(generatedProviderBusinessMethodAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got, want := len(description.Operations), 1; got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}
	if got, want := description.Operations[0].MethodName, "KobaCompiledAPI"; got != want {
		t.Fatalf("method name = %q, want %q", got, want)
	}
}

type panickingCompiledPlanAPI struct{}

func (*panickingCompiledPlanAPI) KobaCompiledAPI() CompiledAPI {
	panic(errors.New("broken generated plan"))
}

func TestCompileAPIReportsGeneratedPlanPanic(t *testing.T) {
	_, err := CompileAPI((*panickingCompiledPlanAPI)(nil))
	if err == nil || !strings.Contains(err.Error(), "broken generated plan") {
		t.Fatalf("CompileAPI() error = %v, want generated-plan panic", err)
	}
}
