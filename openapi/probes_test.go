package openapi

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

func TestGenerateIncludesPublicFrameworkProbes(t *testing.T) {
	document, err := Generate(Config{
		Title:   "Probe API",
		Version: "1.0.0",
		SecuritySchemes: []SecurityScheme{{
			Name:   "bearerAuth",
			Type:   SecurityTypeHTTP,
			Scheme: "bearer",
		}},
		DefaultSecurity: []SecurityRequirement{{"bearerAuth"}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	health := probeOperation(t, document, "/healthz")
	readiness := probeOperation(t, document, "/readyz")
	assertProbeOperation(t, document, health, "healthz_check")
	assertProbeOperation(t, document, readiness, "readyz_check")

	if response := readiness.Responses.Value("503"); response == nil || response.Value == nil {
		t.Fatal("GET /readyz has no 503 response")
	} else {
		assertProbeResponseSchema(t, document, response.Value)
	}
	if health.Responses.Value("503") != nil {
		t.Fatal("GET /healthz unexpectedly documents a 503 response")
	}
	for _, operation := range []*openapi3.Operation{health, readiness} {
		for _, status := range []string{"400", "401", "403"} {
			if operation.Responses.Value(status) != nil {
				t.Errorf("%s unexpectedly documents HTTP %s", operation.OperationID, status)
			}
		}
	}

	response := health.Responses.Value("200")
	media := response.Value.Content["application/json"]
	schemaName := media.Schema.Ref[len("#/components/schemas/"):]
	schema := document.Components.Schemas[schemaName]
	if schema == nil || schema.Value == nil {
		t.Fatalf("probe status component schema %q was not generated", schemaName)
	}
	if !containsString(schema.Value.Required, "status") {
		t.Fatalf("ProbeStatus required fields = %v, want status", schema.Value.Required)
	}
	status := schema.Value.Properties["status"]
	if status == nil || status.Value == nil {
		t.Fatal("ProbeStatus.status schema was not generated")
	}
	wantEnum := []any{"ok", "ready", "not_ready"}
	if !reflect.DeepEqual(status.Value.Enum, wantEnum) {
		t.Errorf("ProbeStatus.status enum = %#v, want %#v", status.Value.Enum, wantEnum)
	}
}

func TestGenerateCanDisableFrameworkProbeDocumentation(t *testing.T) {
	document, err := Generate(Config{
		Title:                "Probe API",
		Version:              "1.0.0",
		DisableDefaultProbes: true,
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if document.Paths.Find(path) != nil {
			t.Errorf("disabled probe %s was generated", path)
		}
	}
}

type explicitHealthAPI struct{}

func (*explicitHealthAPI) Health() fbtypes.ProbeStatus {
	return fbtypes.ProbeStatus{Status: "custom"}
}

func (*explicitHealthAPI) Routes() map[string]string {
	return map[string]string{"Health": "/healthz"}
}

func (*explicitHealthAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Health": {
			OperationID: "custom_health",
			Method:      http.MethodGet,
			Public:      true,
		},
	}}
}

func TestGeneratePreservesExplicitProbeDescription(t *testing.T) {
	document, err := Generate(Config{Title: "Probe API", Version: "1.0.0"}, (*explicitHealthAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if got := probeOperation(t, document, "/healthz").OperationID; got != "custom_health" {
		t.Errorf("GET /healthz operation ID = %q, want custom_health", got)
	}
	if got := probeOperation(t, document, "/readyz").OperationID; got != "readyz_check" {
		t.Errorf("GET /readyz operation ID = %q, want readyz_check", got)
	}
}

func TestBuildRendersOnlyProvidedDescriptions(t *testing.T) {
	document, err := Build(Config{Title: "Low-level API", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	if document.Paths.Len() != 0 {
		t.Fatalf("Build() generated %d paths without descriptions", document.Paths.Len())
	}
}

func TestGenerateFromAppMatchesRuntimeProbeConfiguration(t *testing.T) {
	for _, test := range []struct {
		name       string
		app        fb.KobaApp
		config     Config
		wantProbes bool
	}{
		{name: "runtime defaults", app: fb.MakeRawApp(), config: Config{Title: "Runtime API", Version: "1.0.0"}, wantProbes: true},
		{name: "runtime opt out", app: fb.MakeRawAppWithOptions(fb.WithoutDefaultProbes()), config: Config{Title: "Runtime API", Version: "1.0.0"}},
		{name: "documentation opt out", app: fb.MakeRawApp(), config: Config{Title: "Runtime API", Version: "1.0.0", DisableDefaultProbes: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := GenerateFromApp(test.config, test.app.(fb.WithAPIDescriptions))
			if err != nil {
				t.Fatalf("GenerateFromApp() returned an error: %v", err)
			}
			for _, path := range []string{"/healthz", "/readyz"} {
				if got := document.Paths.Find(path) != nil; got != test.wantProbes {
					t.Errorf("path %s present = %v, want %v", path, got, test.wantProbes)
				}
			}
		})
	}
}

type descriptionRegistryWithoutProbeState struct{}

func (descriptionRegistryWithoutProbeState) RegisteredAPIDescriptions() []fb.APIDescription {
	return nil
}

func TestGenerateFromAppDoesNotInventUnknownRuntimeProbes(t *testing.T) {
	document, err := GenerateFromApp(
		Config{Title: "Unknown runtime API", Version: "1.0.0"},
		descriptionRegistryWithoutProbeState{},
	)
	if err != nil {
		t.Fatalf("GenerateFromApp() returned an error: %v", err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if document.Paths.Find(path) != nil {
			t.Errorf("GenerateFromApp() invented unknown runtime path %s", path)
		}
	}
}

func probeOperation(t *testing.T, document *openapi3.T, path string) *openapi3.Operation {
	t.Helper()
	item := document.Paths.Find(path)
	if item == nil || item.Get == nil {
		t.Fatalf("GET %s was not generated", path)
	}
	return item.Get
}

func assertProbeOperation(t *testing.T, document *openapi3.T, operation *openapi3.Operation, operationID string) {
	t.Helper()
	if operation.OperationID != operationID {
		t.Errorf("operation ID = %q, want %q", operation.OperationID, operationID)
	}
	if !reflect.DeepEqual(operation.Tags, []string{"system"}) {
		t.Errorf("%s tags = %v, want [system]", operationID, operation.Tags)
	}
	if operation.Security == nil || len(*operation.Security) != 0 {
		t.Errorf("%s security = %#v, want explicit public override", operationID, operation.Security)
	}
	response := operation.Responses.Value("200")
	if response == nil || response.Value == nil {
		t.Fatalf("%s has no 200 response", operationID)
	}
	assertProbeResponseSchema(t, document, response.Value)
}

func assertProbeResponseSchema(t *testing.T, document *openapi3.T, response *openapi3.Response) {
	t.Helper()
	media := response.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatal("probe response has no application/json schema")
	}
	assertComponentReference(t, document, media.Schema, reflect.TypeFor[fbtypes.ProbeStatus]())
	if got := media.Schema.Ref; got == "#/components/schemas/ErrorResponse" {
		t.Errorf("probe response uses the generic ErrorResponse schema")
	}
}
