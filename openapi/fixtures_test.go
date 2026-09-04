package openapi

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

type ContractFixtureRequest struct {
	Name     string   `json:"name" binding:"required"`
	Quantity int      `json:"quantity"`
	Labels   []string `json:"labels,omitempty"`
}

type ContractFixtureResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type contractFixtureAPI struct{}

func (*contractFixtureAPI) Update(_ context.Context, request ContractFixtureRequest) ContractFixtureResponse {
	return ContractFixtureResponse{ID: 42, Name: request.Name}
}

type contractTimeResponse struct {
	CreatedAt fbtypes.Time `json:"created_at"`
}

type contractTimeAPI struct{}

func (*contractTimeAPI) Current() contractTimeResponse { return contractTimeResponse{} }

func (*contractFixtureAPI) Routes() map[string]string {
	return map[string]string{"Update": "/v1/widgets/:id"}
}

func (*contractFixtureAPI) APIDocumentation() fb.APIDocumentation {
	requestID := fb.ParameterOf[string]("X-Request-ID", fb.ParameterInHeader, "Request correlation ID", true)
	requestID.Format = "uuid"
	return fb.APIDocumentation{
		Name:        "widgets",
		Description: "Widget management",
		Operations: map[string]fb.OperationDocumentation{
			"Update": {
				OperationID:            "updateWidget",
				Summary:                "Update a widget",
				Description:            "Updates the selected widget.",
				Tags:                   []string{"inventory"},
				RequestBodyDescription: "New widget values",
				Parameters: []fb.ParameterDocumentation{
					fb.ParameterOf[int]("id", fb.ParameterInPath, "Widget ID", false),
					fb.ParameterOf[bool]("dry_run", fb.ParameterInQuery, "Validate without saving", false),
					requestID,
				},
				Responses: map[int]fb.ResponseDocumentation{
					http.StatusOK:                  {Description: "Updated widget"},
					http.StatusUnprocessableEntity: {Description: "Widget validation failed"},
				},
				Security: []fb.SecurityRequirement{{"tokenAuth"}},
			},
		},
	}
}

type contractCustomID struct {
	Value int
}

func (contractCustomID) MarshalJSON() ([]byte, error) { return []byte(`"custom-id"`), nil }

type contractCustomResponse struct {
	ID contractCustomID `json:"id"`
}

type contractCustomAPI struct{}

func (*contractCustomAPI) Get() contractCustomResponse { return contractCustomResponse{} }

type contractEmptyAPI struct{}

func (*contractEmptyAPI) Run() fb.Empty { return fb.Empty{} }

type methodFixtureAPI struct{}

func (*methodFixtureAPI) Replace(ContractFixtureRequest) ContractFixtureResponse {
	return ContractFixtureResponse{}
}

func (*methodFixtureAPI) Delete() {}

func (*methodFixtureAPI) Routes() map[string]string {
	return map[string]string{
		"Replace": "/v1/methods/:id",
		"Delete":  "/v1/methods/:id",
	}
}

func (*methodFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Replace": {
			Method:     http.MethodPut,
			Parameters: []fb.ParameterDocumentation{fb.ParameterOf[string]("id", fb.ParameterInPath, "Method ID", true)},
		},
		"Delete": {
			Method:     http.MethodDelete,
			Parameters: []fb.ParameterDocumentation{fb.ParameterOf[string]("id", fb.ParameterInPath, "Method ID", true)},
		},
	}}
}

func assertComponentReference(t *testing.T, document *openapi3.T, schema *openapi3.SchemaRef, valueType reflect.Type) {
	t.Helper()
	name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
	if name == schema.Ref || document.Components.Schemas[name] == nil {
		t.Fatalf("schema %v for %v does not reference a generated component", schema, valueType)
	}
}

func assertParameter(t *testing.T, parameter *openapi3.Parameter, location string, required bool) {
	t.Helper()
	if parameter == nil {
		t.Fatalf("%s parameter was not generated", location)
	}
	if got := parameter.In; got != location {
		t.Errorf("parameter %q location = %q, want %q", parameter.Name, got, location)
	}
	if got := parameter.Required; got != required {
		t.Errorf("parameter %q required = %v, want %v", parameter.Name, got, required)
	}
}

func assertResponseDescription(t *testing.T, operation *openapi3.Operation, status int, want string) {
	t.Helper()
	response := operation.Responses.Value(strconv.Itoa(status))
	if response == nil || response.Value == nil || response.Value.Description == nil {
		t.Fatalf("response %d was not generated", status)
	}
	if got := *response.Value.Description; got != want {
		t.Errorf("response %d description = %q, want %q", status, got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
