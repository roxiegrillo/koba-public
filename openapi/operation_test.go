package openapi

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

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

func TestBuildRendersResponseHeadersAndOptionalAuthentication(t *testing.T) {
	document, err := Build(Config{
		Title:   "Optional API",
		Version: "1",
		SecuritySchemes: []SecurityScheme{{
			Name:   "bearerAuth",
			Type:   SecurityTypeHTTP,
			Scheme: "bearer",
		}},
		DefaultSecurity: []SecurityRequirement{{"bearerAuth"}},
	}, []fb.APIDescription{{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "items_list",
		Route:       "/items",
		HTTPMethod:  http.MethodGet,
		InputType: reflect.TypeFor[struct {
			Filter string `json:"filter"`
		}](),
		OutputType: reflect.TypeFor[[]string](),
		Documentation: fb.OperationDocumentation{
			Security: []fb.SecurityRequirement{{"bearerAuth"}, {}},
			Responses: map[int]fb.ResponseDocumentation{
				http.StatusOK: {
					Description: "Items page",
					Headers: map[string]fb.HeaderDocumentation{
						"X-Next-Page-Token": {
							Description: "Opaque continuation token",
							Required:    true,
							Format:      "opaque-token",
							Pattern:     `^[A-Za-z0-9_-]+$`,
							Enum:        []string{"next_page_1", "next_page_2"},
							Example:     "next_page_1",
						},
					},
				},
				http.StatusBadRequest: {
					Headers: map[string]fb.HeaderDocumentation{
						"X-Request-ID": {Description: "Request correlation ID"},
					},
				},
			},
		},
	}}}})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}

	operation := document.Paths.Find("/items").Get
	if operation.Security == nil || len(*operation.Security) != 2 || len((*operation.Security)[1]) != 0 {
		t.Fatalf("optional operation security = %#v", operation.Security)
	}
	assertStringResponseHeader(t, operation, http.StatusOK, "X-Next-Page-Token", "Opaque continuation token", true)
	assertStringResponseHeader(t, operation, http.StatusBadRequest, "X-Request-ID", "Request correlation ID", false)
	nextPageHeader := operation.Responses.Value("200").Value.Headers["X-Next-Page-Token"].Value
	if got := nextPageHeader.Schema.Value.Format; got != "opaque-token" {
		t.Errorf("header format = %q, want opaque-token", got)
	}
	if got := nextPageHeader.Schema.Value.Pattern; got != `^[A-Za-z0-9_-]+$` {
		t.Errorf("header pattern = %q", got)
	}
	if got := nextPageHeader.Schema.Value.Enum; len(got) != 2 || got[0] != "next_page_1" || got[1] != "next_page_2" {
		t.Errorf("header enum = %#v", got)
	}
	if got := nextPageHeader.Example; got != "next_page_1" {
		t.Errorf("header example = %#v, want next_page_1", got)
	}
}

func TestBuildPreservesExplicitEmptySecurityOverride(t *testing.T) {
	document, err := Build(Config{
		Title:   "Public override API",
		Version: "1",
		SecuritySchemes: []SecurityScheme{{
			Name:   "bearerAuth",
			Type:   SecurityTypeHTTP,
			Scheme: "bearer",
		}},
		DefaultSecurity: []SecurityRequirement{{"bearerAuth"}},
	}, []fb.APIDescription{{Operations: []fb.OperationDescription{{
		MethodName:  "Health",
		OperationID: "health",
		Route:       "/health",
		HTTPMethod:  http.MethodGet,
		Documentation: fb.OperationDocumentation{
			Security: []fb.SecurityRequirement{},
		},
	}}}})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	security := document.Paths.Find("/health").Get.Security
	if security == nil || len(*security) != 0 {
		t.Fatalf("explicit empty security override = %#v, want non-nil empty requirements", security)
	}
}

func assertStringResponseHeader(t *testing.T, operation *openapi3.Operation, status int, name, description string, required bool) {
	t.Helper()
	response := operation.Responses.Value(strconv.Itoa(status))
	if response == nil || response.Value == nil {
		t.Fatalf("response %d is missing", status)
	}
	header := response.Value.Headers[name]
	if header == nil || header.Value == nil {
		t.Fatalf("response %d header %q is missing", status, name)
	}
	if got := header.Value.Description; got != description {
		t.Errorf("response %d header %q description = %q, want %q", status, name, got, description)
	}
	if got := header.Value.Required; got != required {
		t.Errorf("response %d header %q required = %v, want %v", status, name, got, required)
	}
	if schema := header.Value.Schema; schema == nil || schema.Value == nil || schema.Value.Type == nil || !schema.Value.Type.Is("string") {
		t.Errorf("response %d header %q schema = %#v, want string", status, name, schema)
	}
}

func TestBuildRendersRequiredScopesAndAuthorizationResponses(t *testing.T) {
	document, err := Build(Config{
		Title:   "Scoped API",
		Version: "1",
		SecuritySchemes: []SecurityScheme{{
			Name:         "bearerAuth",
			Type:         SecurityTypeHTTP,
			Scheme:       "bearer",
			BearerFormat: "JWT",
		}},
		DefaultSecurity: []SecurityRequirement{{"bearerAuth"}},
	}, []fb.APIDescription{{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "notes_list",
		Route:       "/notes",
		HTTPMethod:  http.MethodGet,
		Contract: fb.OperationContract{
			Authorization: fb.AuthorizationContract{
				RequiredScopes: []string{"read:notes", "read:profile"},
			},
		},
	}}}})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/notes").Get
	if operation.Security == nil || len(*operation.Security) != 1 {
		t.Fatalf("operation security = %#v", operation.Security)
	}
	if got := (*operation.Security)[0]["bearerAuth"]; len(got) != 0 {
		t.Fatalf("bearer security scopes = %v, want an empty OpenAPI scope array", got)
	}
	gotScopes, ok := operation.Extensions["x-required-scopes"].([]string)
	if want := []string{"read:notes", "read:profile"}; !ok || len(gotScopes) != len(want) || gotScopes[0] != want[0] || gotScopes[1] != want[1] {
		t.Fatalf("x-required-scopes = %#v, want %v", operation.Extensions["x-required-scopes"], want)
	}
	for _, status := range []string{"401", "403"} {
		if response := operation.Responses.Value(status); response == nil || response.Value == nil {
			t.Errorf("response %s is missing", status)
		}
	}
}

func TestBuildRejectsRequiredScopesWithoutSecurity(t *testing.T) {
	description := []fb.APIDescription{{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "notes_list",
		Route:       "/notes",
		HTTPMethod:  http.MethodGet,
		Documentation: fb.OperationDocumentation{
			RequiredScopes: []string{"read:notes"},
		},
	}}}}

	_, err := Build(Config{Title: "Scoped API", Version: "1"}, description)
	if err == nil {
		t.Fatal("Build() succeeded without an effective security requirement")
	}
}
