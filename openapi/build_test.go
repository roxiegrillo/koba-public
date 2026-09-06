package openapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

func TestBuildUsesDocumentMetadataOwnedByAPIDocumentation(t *testing.T) {
	document, err := Build(Config{}, []fb.APIDescription{{
		Document: &fb.APIDocument{
			Title:           "Document-owned API",
			Version:         "2.0.0",
			Description:     "Owned beside the functional API.",
			ExternalDocsURL: "https://example.com/docs",
			Servers: []fb.APIServer{
				{URL: "/", Description: "Same origin"},
			},
			SecuritySchemes: []fb.APISecurityScheme{
				{Name: "bearerAuth", Type: fb.SecurityTypeHTTP, Scheme: "bearer"},
			},
			DefaultSecurity: []fb.SecurityRequirement{{"bearerAuth"}},
		},
	}})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	if got, want := document.Info.Title, "Document-owned API"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := document.Info.Version, "2.0.0"; got != want {
		t.Errorf("version = %q, want %q", got, want)
	}
	if len(document.Servers) != 1 || document.Servers[0].URL != "/" {
		t.Errorf("servers = %#v, want same-origin server", document.Servers)
	}
	if document.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Error("bearerAuth scheme is missing")
	}
}

func TestBuildRejectsTwoDocumentMetadataOwners(t *testing.T) {
	_, err := Build(Config{}, []fb.APIDescription{
		{Document: &fb.APIDocument{Title: "First", Version: "1.0.0"}},
		{Document: &fb.APIDocument{Title: "Second", Version: "1.0.0"}},
	})
	if err == nil || !strings.Contains(err.Error(), "more than one API") {
		t.Fatalf("Build() error = %v, want duplicate document owner", err)
	}
}

func TestBuildRejectsDocumentMetadataInConfigAndAPIDocumentation(t *testing.T) {
	_, err := Build(Config{Title: "Legacy", Version: "1.0.0"}, []fb.APIDescription{{
		Document: &fb.APIDocument{Title: "Owned", Version: "1.0.0"},
	}})
	if err == nil || !strings.Contains(err.Error(), "either APIDocumentation or Config") {
		t.Fatalf("Build() error = %v, want duplicate source error", err)
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
