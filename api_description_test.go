package koba

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type descriptionFixtureRequest struct {
	Name string `json:"name"`
}

type descriptionFixtureResponse struct {
	ID int `json:"id"`
}

type descriptionFixtureAPI struct{}

func (*descriptionFixtureAPI) Create(_ context.Context, _ descriptionFixtureRequest) descriptionFixtureResponse {
	return descriptionFixtureResponse{}
}

func (*descriptionFixtureAPI) Routes() map[string]string {
	return map[string]string{"Create": "/v1/widgets"}
}

func (*descriptionFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{
		Name: "widgets",
		Operations: map[string]OperationDocumentation{
			"Create": {
				OperationID: "createWidget",
				Summary:     "Create a widget",
			},
		},
	}
}

func TestDescribeAPIExcludesMetadataMethodsAndAcceptsTypedNil(t *testing.T) {
	var api *descriptionFixtureAPI

	description, err := DescribeAPI(api)
	if err != nil {
		t.Fatalf("DescribeAPI(typed nil) returned an error: %v", err)
	}
	if got, want := description.Name, "widgets"; got != want {
		t.Fatalf("description name = %q, want %q", got, want)
	}
	if got, want := len(description.Operations), 1; got != want {
		t.Fatalf("operation count = %d, want %d: %#v", got, want, description.Operations)
	}

	operation := description.Operations[0]
	if got, want := operation.MethodName, "Create"; got != want {
		t.Errorf("method name = %q, want %q", got, want)
	}
	if got, want := operation.OperationID, "createWidget"; got != want {
		t.Errorf("operation ID = %q, want %q", got, want)
	}
	if got, want := operation.Route, "/v1/widgets"; got != want {
		t.Errorf("route = %q, want %q", got, want)
	}
	if operation.MethodName == "Routes" || operation.MethodName == "APIDocumentation" {
		t.Fatalf("metadata method %q was exposed as an endpoint", operation.MethodName)
	}
	if got, want := operation.InputType, reflect.TypeFor[descriptionFixtureRequest](); got != want {
		t.Errorf("input type = %v, want %v", got, want)
	}
	if got, want := operation.OutputType, reflect.TypeFor[descriptionFixtureResponse](); got != want {
		t.Errorf("output type = %v, want %v", got, want)
	}
}

type validationFixtureAPI struct {
	documentation APIDocumentation
}

func (validationFixtureAPI) First() {}

func (validationFixtureAPI) Second() {}

func (api validationFixtureAPI) APIDocumentation() APIDocumentation {
	return api.documentation
}

func TestDescribeAPIRejectsDuplicateAndInvalidMetadata(t *testing.T) {
	tests := []struct {
		name          string
		documentation APIDocumentation
		wantError     string
	}{
		{
			name: "duplicate operation IDs",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First":  {OperationID: "sameOperation"},
				"Second": {OperationID: "sameOperation"},
			}},
			wantError: "duplicate operation ID",
		},
		{
			name: "unknown documented method",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"Missing": {Summary: "Not an endpoint"},
			}},
			wantError: "unknown API method",
		},
		{
			name: "invalid parameter location",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{{
					Name:   "filter",
					In:     ParameterLocation("body"),
					GoType: reflect.TypeFor[string](),
				}}},
			}},
			wantError: "invalid location",
		},
		{
			name: "untyped parameter",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{{
					Name: "filter",
					In:   ParameterInQuery,
				}}},
			}},
			wantError: "has no Go type",
		},
		{
			name: "unsupported success status",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					201: {Description: "Created"},
				}},
			}},
			wantError: "resolved success status",
		},
		{
			name: "case-insensitive duplicate headers",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{
					ParameterOf[string]("X-Request-ID", ParameterInHeader, "", false),
					ParameterOf[string]("x-request-id", ParameterInHeader, "", false),
				}},
			}},
			wantError: "duplicate parameter",
		},
		{
			name: "public with security override",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {
					Public:   true,
					Security: []SecurityRequirement{{"token"}},
				},
			}},
			wantError: "both Public and Security",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeAPI(validationFixtureAPI{documentation: test.documentation})
			if err == nil {
				t.Fatalf("DescribeAPI() succeeded, want an error containing %q", test.wantError)
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DescribeAPI() error = %q, want it to contain %q", err, test.wantError)
			}
		})
	}
}

type firstDefaultOperationAPI struct{}

func (firstDefaultOperationAPI) Get() {}

type secondDefaultOperationAPI struct{}

func (secondDefaultOperationAPI) Get() {}

func TestDescribeAPIQualifiesDefaultOperationIDs(t *testing.T) {
	first, err := DescribeAPI(firstDefaultOperationAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI(first) returned an error: %v", err)
	}
	second, err := DescribeAPI(secondDefaultOperationAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI(second) returned an error: %v", err)
	}
	if first.Operations[0].OperationID == second.Operations[0].OperationID {
		t.Fatalf("default operation IDs collide: %q", first.Operations[0].OperationID)
	}
	if got, want := first.Operations[0].OperationID, "githubComRoxiegrilloKobaV2FirstDefaultOperationAPIGet"; got != want {
		t.Fatalf("first default operation ID = %q, want %q", got, want)
	}
}

type successStatusFixtureAPI struct {
	documentation OperationDocumentation
}

func (successStatusFixtureAPI) Get() descriptionFixtureResponse {
	return descriptionFixtureResponse{}
}

func (api successStatusFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{"Get": api.documentation}}
}

func TestDescribeAPIValidatesSuccessStatusSemantics(t *testing.T) {
	tests := []struct {
		name          string
		documentation OperationDocumentation
		want          string
	}{
		{name: "status below 200", documentation: OperationDocumentation{SuccessStatus: 199}, want: "between 200 and 299"},
		{name: "status above 299", documentation: OperationDocumentation{SuccessStatus: 300}, want: "between 200 and 299"},
		{name: "204 with response", documentation: OperationDocumentation{SuccessStatus: http.StatusNoContent}, want: "cannot be used by an operation with a response value"},
		{name: "205 with response", documentation: OperationDocumentation{SuccessStatus: http.StatusResetContent}, want: "cannot be used by an operation with a response value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeAPI(successStatusFixtureAPI{documentation: test.documentation})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DescribeAPI() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

type businessDocumentationMethodAPI struct{}

func (businessDocumentationMethodAPI) APIDocumentation() string { return "business response" }

func TestBusinessMethodNamedAPIDocumentationRemainsAnEndpoint(t *testing.T) {
	description, err := DescribeAPI(businessDocumentationMethodAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got, want := len(description.Operations), 1; got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}
	if got, want := description.Operations[0].MethodName, "APIDocumentation"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
}

type emptyRouteFixtureAPI struct{}

func (emptyRouteFixtureAPI) Get() {}

func (emptyRouteFixtureAPI) Routes() map[string]string {
	return map[string]string{"Get": ""}
}

func TestDescribeAPIPreservesExplicitEmptyRouteOverride(t *testing.T) {
	description, err := DescribeAPI(emptyRouteFixtureAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got := description.Operations[0].Route; got != "" {
		t.Fatalf("explicit route = %q, want empty", got)
	}
}
