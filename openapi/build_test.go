package openapi

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

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
