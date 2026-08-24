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

func TestGenerateRendersSchemasParametersSecurityAndStatuses(t *testing.T) {
	document, err := Generate(Config{
		Title:   "Widget API",
		Version: "1.2.3",
		SecuritySchemes: []SecurityScheme{{
			Name:        "tokenAuth",
			Type:        SecurityTypeAPIKey,
			In:          openapi3.ParameterInHeader,
			Parameter:   "Authorization",
			ValuePrefix: "Token ",
		}},
		DefaultSecurity: []SecurityRequirement{{"tokenAuth"}},
	}, (*contractFixtureAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	pathItem := document.Paths.Find("/v1/widgets/{id}")
	if pathItem == nil || pathItem.Post == nil {
		t.Fatalf("POST /v1/widgets/{id} was not generated: %#v", pathItem)
	}
	operation := pathItem.Post
	if got, want := operation.OperationID, "updateWidget"; got != want {
		t.Errorf("operation ID = %q, want %q", got, want)
	}

	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		t.Fatal("request body was not generated")
	}
	requestMedia := operation.RequestBody.Value.Content["application/json"]
	if requestMedia == nil || requestMedia.Schema == nil || requestMedia.Schema.Ref == "" {
		t.Fatalf("request schema is not a component reference: %#v", requestMedia)
	}
	assertComponentReference(t, document, requestMedia.Schema, reflect.TypeFor[ContractFixtureRequest]())
	if !containsString(requestMedia.Schema.Value.Required, "name") {
		t.Fatalf("request required fields = %v, want name", requestMedia.Schema.Value.Required)
	}

	response := operation.Responses.Value("200")
	if response == nil || response.Value == nil {
		t.Fatal("200 response was not generated")
	}
	responseMedia := response.Value.Content["application/json"]
	if responseMedia == nil || responseMedia.Schema == nil || responseMedia.Schema.Ref == "" {
		t.Fatalf("response schema is not a component reference: %#v", responseMedia)
	}
	assertComponentReference(t, document, responseMedia.Schema, reflect.TypeFor[ContractFixtureResponse]())
	assertResponseDescription(t, operation, http.StatusOK, "Updated widget")

	parameters := make(map[string]*openapi3.Parameter)
	for _, parameter := range operation.Parameters {
		if parameter.Value != nil {
			parameters[parameter.Value.Name] = parameter.Value
		}
	}
	assertParameter(t, parameters["id"], openapi3.ParameterInPath, true)
	assertParameter(t, parameters["dry_run"], openapi3.ParameterInQuery, false)
	assertParameter(t, parameters["X-Request-ID"], openapi3.ParameterInHeader, true)
	if got, want := parameters["X-Request-ID"].Schema.Value.Format, "uuid"; got != want {
		t.Errorf("header format = %q, want %q", got, want)
	}

	if operation.Security == nil || len(*operation.Security) != 1 {
		t.Fatalf("operation security = %#v, want tokenAuth", operation.Security)
	}
	if _, exists := (*operation.Security)[0]["tokenAuth"]; !exists {
		t.Fatalf("operation security = %#v, want tokenAuth", *operation.Security)
	}
	scheme := document.Components.SecuritySchemes["tokenAuth"]
	if scheme == nil || scheme.Value == nil {
		t.Fatal("tokenAuth security scheme was not generated")
	}
	if got, want := scheme.Value.Extensions["x-auth-value-prefix"], "Token "; got != want {
		t.Errorf("x-auth-value-prefix = %#v, want %#v", got, want)
	}

	assertResponseDescription(t, operation, http.StatusUnprocessableEntity, "Widget validation failed")
}

func TestSchemaForTypeRegistersTopLevelComponent(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := schemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[ContractFixtureRequest]())
	if err != nil {
		t.Fatalf("schemaForType() returned an error: %v", err)
	}
	t.Logf("ref=%#v schemas=%#v", ref, schemas)
	if ref.Ref == "" {
		t.Fatal("schemaForType() did not return a component reference")
	}
	name := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	if schemas[name] == nil {
		t.Fatalf("schemaForType() returned %q but did not register component %q", ref.Ref, name)
	}
}

func TestSchemaForTypeSupportsUnconstrainedInterfaces(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := schemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[any]())
	if err != nil {
		t.Fatalf("schemaForType(any) returned an error: %v", err)
	}
	if ref == nil || ref.Value == nil {
		t.Fatalf("schemaForType(any) = %#v, want an inline unconstrained schema", ref)
	}
	if ref.Ref != "" || ref.Value.Type != nil {
		t.Fatalf("schemaForType(any) = %#v, want an unconstrained schema", ref)
	}
}

func TestSchemaForTypePreservesPointerNullability(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := responseSchemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[*ContractFixtureResponse]())
	if err != nil {
		t.Fatalf("responseSchemaForType(*ContractFixtureResponse) returned an error: %v", err)
	}
	if ref == nil || ref.Value == nil || !ref.Value.Nullable {
		t.Fatalf("pointer schema = %#v, want nullable wrapper", ref)
	}
	if len(ref.Value.AllOf) != 1 || ref.Value.AllOf[0] == nil || ref.Value.AllOf[0].Ref == "" {
		t.Fatalf("pointer schema allOf = %#v, want one component reference", ref.Value.AllOf)
	}
}

func TestErrorResponseSchemaIsFullyDocumented(t *testing.T) {
	schema := errorResponseSchema()
	if schema.Description == "" {
		t.Fatal("ErrorResponse schema has no description")
	}
	for _, propertyName := range []string{"status", "error", "message"} {
		property := schema.Properties[propertyName]
		if property == nil || property.Value == nil || property.Value.Description == "" {
			t.Errorf("ErrorResponse.%s has no description", propertyName)
		}
	}
}

func TestGenerateDescribesKobaTimeAsDateTime(t *testing.T) {
	document, err := Generate(Config{Title: "Time API", Version: "1"}, (*contractTimeAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	operation := document.Paths.Find("/Current").Get
	response := operation.Responses.Value("200").Value.Content["application/json"]
	component := response.Schema.Value
	property := component.Properties["created_at"]
	if property == nil || property.Value == nil {
		t.Fatalf("created_at schema = %#v", property)
	}
	if got, want := property.Value.Format, "date-time"; got != want {
		t.Fatalf("created_at format = %q, want %q", got, want)
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

func TestGenerateSupportsProjectSchemaCustomization(t *testing.T) {
	document, err := Generate(Config{
		Title:   "Custom API",
		Version: "1",
		SchemaCustomizer: func(context SchemaContext) error {
			if context.GoType == reflect.TypeFor[contractCustomID]() {
				*context.Schema = *openapi3.NewStringSchema()
				context.Schema.Format = "custom-id"
			}
			return nil
		},
	}, (*contractCustomAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	operation := document.Paths.Find("/Get").Get
	property := operation.Responses.Value("200").Value.Content["application/json"].Schema.Value.Properties["id"]
	if property == nil || property.Value == nil {
		t.Fatalf("custom ID schema = %#v", property)
	}
	if got, want := property.Value.Format, "custom-id"; got != want {
		t.Fatalf("custom ID format = %q, want %q", got, want)
	}
}

type contractEmptyAPI struct{}

func (*contractEmptyAPI) Run() fb.Empty { return fb.Empty{} }

func TestGenerateSupportsEmptyStructResponses(t *testing.T) {
	document, err := Generate(Config{Title: "Empty API", Version: "1"}, (*contractEmptyAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	schema := document.Paths.Find("/Run").Get.Responses.Value("200").Value.Content["application/json"].Schema
	if schema == nil || schema.Value == nil || schema.Value.Type == nil || !schema.Value.Type.Is("object") {
		t.Fatalf("empty response schema = %#v, want object", schema)
	}
}

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

func TestGenerateSupportsPutDeleteAndVoid204(t *testing.T) {
	document, err := Generate(Config{Title: "Methods API", Version: "1"}, (*methodFixtureAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	item := document.Paths.Find("/v1/methods/{id}")
	if item == nil || item.Put == nil || item.Delete == nil {
		t.Fatalf("PUT and DELETE operations were not generated: %#v", item)
	}
	if response := item.Put.Responses.Value("200"); response == nil || response.Value == nil {
		t.Fatal("PUT 200 response was not generated")
	}
	response := item.Delete.Responses.Value("204")
	if response == nil || response.Value == nil {
		t.Fatal("DELETE 204 response was not generated")
	}
	if len(response.Value.Content) != 0 {
		t.Fatalf("DELETE 204 content = %#v, want no response body", response.Value.Content)
	}
}

func TestRenderSecurityRequirementsPreservesAndOrSemantics(t *testing.T) {
	known := map[string]struct{}{"token": {}, "signature": {}, "fallback": {}}
	requirements, err := renderSecurityRequirements([]SecurityRequirement{
		{"token", "signature"},
		{"fallback"},
	}, known)
	if err != nil {
		t.Fatalf("renderSecurityRequirements() returned an error: %v", err)
	}
	if got, want := len(requirements), 2; got != want {
		t.Fatalf("alternative count = %d, want %d", got, want)
	}
	if _, exists := requirements[0]["token"]; !exists {
		t.Fatal("first alternative does not require token")
	}
	if _, exists := requirements[0]["signature"]; !exists {
		t.Fatal("first alternative does not require signature")
	}
	if _, exists := requirements[1]["fallback"]; !exists {
		t.Fatal("second alternative does not require fallback")
	}
}

func TestGenerateRejectsDuplicateAndInvalidOperationMetadata(t *testing.T) {
	t.Run("normalizes a direct description HTTP method", func(t *testing.T) {
		document, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:  "Get",
				OperationID: "getWidget",
				Route:       "/widgets",
				HTTPMethod:  " get ",
			}},
		}})
		if err != nil {
			t.Fatalf("Build() returned an error: %v", err)
		}
		if operation := document.Paths.Find("/widgets").Get; operation == nil || operation.OperationID != "getWidget" {
			t.Fatalf("normalized GET operation = %#v", operation)
		}
	})

	t.Run("duplicate parameter", func(t *testing.T) {
		description, err := fb.DescribeAPI((*contractFixtureAPI)(nil))
		if err != nil {
			t.Fatalf("DescribeAPI() returned an error: %v", err)
		}
		description.Operations[0].Documentation.Parameters = append(
			description.Operations[0].Documentation.Parameters,
			fb.ParameterOf[int]("id", fb.ParameterInPath, "Duplicate widget ID", true),
		)
		_, err = Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{description})
		if err == nil || !strings.Contains(err.Error(), "duplicate parameter") {
			t.Fatalf("Build() error = %v, want duplicate parameter error", err)
		}
	})

	t.Run("unknown security scheme", func(t *testing.T) {
		_, err := Generate(Config{Title: "Widgets", Version: "1"}, (*contractFixtureAPI)(nil))
		if err == nil || !strings.Contains(err.Error(), "unknown security scheme") {
			t.Fatalf("Generate() error = %v, want unknown security scheme error", err)
		}
	})

	t.Run("unsupported success status", func(t *testing.T) {
		description, err := fb.DescribeAPI((*contractFixtureAPI)(nil))
		if err != nil {
			t.Fatalf("DescribeAPI() returned an error: %v", err)
		}
		description.Operations[0].Documentation.Responses[http.StatusCreated] = fb.ResponseDocumentation{
			Description: "Created",
		}
		_, err = Build(Config{
			Title:   "Widgets",
			Version: "1",
			SecuritySchemes: []SecurityScheme{{
				Name:      "tokenAuth",
				Type:      SecurityTypeAPIKey,
				In:        openapi3.ParameterInHeader,
				Parameter: "Authorization",
			}},
		}, []fb.APIDescription{description})
		if err == nil || !strings.Contains(err.Error(), "resolved success status") {
			t.Fatalf("Build() error = %v, want unsupported success status error", err)
		}
	})

	t.Run("unsupported HTTP method", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:  "Trace",
				OperationID: "traceWidget",
				Route:       "/widgets",
				HTTPMethod:  http.MethodTrace,
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "supported methods are GET, POST, PUT, PATCH, and DELETE") {
			t.Fatalf("Build() error = %v, want unsupported HTTP method error", err)
		}
	})

	t.Run("direct description rejects non-2xx success", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:    "Get",
				OperationID:   "invalidStatusGet",
				Route:         "/widgets",
				HTTPMethod:    http.MethodGet,
				Documentation: fb.OperationDocumentation{SuccessStatus: 300},
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "between 200 and 299") {
			t.Fatalf("Build() error = %v, want invalid success status error", err)
		}
	})

	t.Run("direct description rejects 204 with response", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:    "Get",
				OperationID:   "invalidNoContentGet",
				Route:         "/widgets",
				HTTPMethod:    http.MethodGet,
				OutputType:    reflect.TypeFor[ContractFixtureResponse](),
				Documentation: fb.OperationDocumentation{SuccessStatus: http.StatusNoContent},
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "cannot be used by an operation with a response value") {
			t.Fatalf("Build() error = %v, want body-forbidden status error", err)
		}
	})
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
