package kobaapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/openapi"
)

type definitionFixtureRequest struct {
	Name string `json:"name" binding:"required"`
}

type definitionFixtureResponse struct {
	ID string `json:"id"`
}

type definitionFixtureAPI struct{}

func (*definitionFixtureAPI) Create(_ context.Context, request definitionFixtureRequest) definitionFixtureResponse {
	return definitionFixtureResponse{ID: request.Name}
}

func (*definitionFixtureAPI) Routes() map[string]string {
	return map[string]string{"Create": "/items"}
}

func (*definitionFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name: "items",
		Operations: map[string]fb.OperationDocumentation{
			"Create": {
				OperationID: "itemsCreate",
				Summary:     "Create an item",
				Tags:        []string{"items"},
				Public:      true,
			},
		},
	}
}

func TestGenerateAndCheckOwnAllArtifacts(t *testing.T) {
	directory := t.TempDir()
	definition := fixtureDefinition(directory)

	if err := Generate(definition); err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if err := Check(definition); err != nil {
		t.Fatalf("Check() returned an error after generation: %v", err)
	}

	manifestContents, err := os.ReadFile(definition.ManifestOutput)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestContents, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if got, want := manifest.ServiceID, "fixture"; got != want {
		t.Fatalf("manifest service = %q, want %q", got, want)
	}
	if got, want := len(manifest.Artifacts), 2; got != want {
		t.Fatalf("manifest artifacts = %d, want %d", got, want)
	}
	for _, artifact := range manifest.Artifacts {
		if !strings.HasPrefix(artifact.SHA256, "sha256:") {
			t.Errorf("artifact %s digest = %q", artifact.Path, artifact.SHA256)
		}
	}

	staticContents, err := os.ReadFile(definition.StaticTargets[0].Output)
	if err != nil {
		t.Fatalf("read static bindings: %v", err)
	}
	if !strings.Contains(string(staticContents), "api.Create(invocation.Context, request)") {
		t.Fatalf("static bindings do not contain direct method call:\n%s", staticContents)
	}
}

func TestCheckIsReadOnlyAndReportsStaleArtifact(t *testing.T) {
	directory := t.TempDir()
	definition := fixtureDefinition(directory)
	if err := Generate(definition); err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if err := os.WriteFile(definition.OpenAPIOutput, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("make contract stale: %v", err)
	}

	err := Check(definition)
	if err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("Check() error = %v, want stale artifact error", err)
	}
	contents, readErr := os.ReadFile(definition.OpenAPIOutput)
	if readErr != nil {
		t.Fatalf("read stale contract: %v", readErr)
	}
	if got, want := string(contents), "stale\n"; got != want {
		t.Fatalf("Check() mutated contract to %q", got)
	}
}

func TestCommandModes(t *testing.T) {
	directory := t.TempDir()
	definition := fixtureDefinition(directory)

	validate := NewCommand(definition)
	validate.SetArgs([]string{"--validate"})
	if err := validate.Execute(); err != nil {
		t.Fatalf("--validate returned an error: %v", err)
	}
	if _, err := os.Stat(definition.OpenAPIOutput); !os.IsNotExist(err) {
		t.Fatalf("--validate wrote an artifact: %v", err)
	}

	generate := NewCommand(definition)
	if err := generate.Execute(); err != nil {
		t.Fatalf("generate command returned an error: %v", err)
	}
	check := NewCommand(definition)
	check.SetArgs([]string{"--check"})
	if err := check.Execute(); err != nil {
		t.Fatalf("--check returned an error: %v", err)
	}

	invalid := NewCommand(definition)
	invalid.SetArgs([]string{"--check", "--validate"})
	if err := invalid.Execute(); err == nil {
		t.Fatal("--check --validate succeeded, want conflict error")
	}
}

func TestDefinitionRejectsDuplicateOutputs(t *testing.T) {
	definition := fixtureDefinition(t.TempDir())
	definition.StaticTargets[0].Output = definition.OpenAPIOutput
	err := Validate(definition)
	if err == nil || !strings.Contains(err.Error(), "used by both") {
		t.Fatalf("Validate() error = %v, want duplicate output error", err)
	}
}

func TestDefinitionRequiresEveryAPIInExactlyOneStaticTarget(t *testing.T) {
	definition := fixtureDefinition(t.TempDir())
	definition.StaticTargets[0].APIs = nil
	err := Validate(definition)
	if err == nil || !strings.Contains(err.Error(), "at least one API is required") {
		t.Fatalf("Validate() error = %v, want empty static target error", err)
	}

	definition = fixtureDefinition(t.TempDir())
	definition.StaticTargets = append(definition.StaticTargets, StaticTarget{
		PackageName: "kobaapp",
		PackagePath: "github.com/roxiegrillo/koba/v2/kobaapp",
		Output:      filepath.Join(t.TempDir(), "duplicate.gen.go"),
		APIs:        definition.APIs,
	})
	err = Validate(definition)
	if err == nil || !strings.Contains(err.Error(), "more than one static target") {
		t.Fatalf("Validate() error = %v, want duplicate static coverage error", err)
	}
}

func TestStaticTargetUsesCanonicalDefinitionAPIValue(t *testing.T) {
	directory := t.TempDir()
	definition := fixtureDefinition(directory)
	// StaticTargets select ownership by API identity. Generation must still use
	// the canonical pointer value from Definition.APIs so pointer-only methods
	// and metadata cannot disappear from the provider.
	definition.StaticTargets[0].APIs = []any{definitionFixtureAPI{}}
	if err := Generate(definition); err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	contents, err := os.ReadFile(definition.StaticTargets[0].Output)
	if err != nil {
		t.Fatalf("read generated provider: %v", err)
	}
	source := string(contents)
	if !strings.Contains(source, "func (api *definitionFixtureAPI) KobaCompiledAPI()") ||
		!strings.Contains(source, "api.Create(invocation.Context, request)") {
		t.Fatalf("generated provider did not use canonical pointer API:\n%s", source)
	}
}

func fixtureDefinition(directory string) Definition {
	api := (*definitionFixtureAPI)(nil)
	return Definition{
		ID:             "fixture",
		APIs:           []any{api},
		OpenAPI:        openapi.Config{Title: "Fixture API", Version: "1.0.0"},
		OpenAPIOutput:  filepath.Join(directory, "contract", "openapi.json"),
		ManifestOutput: filepath.Join(directory, "contract", "koba.lock.json"),
		StaticTargets: []StaticTarget{{
			PackageName: "kobaapp",
			PackagePath: "github.com/roxiegrillo/koba/v2/kobaapp",
			Output:      filepath.Join(directory, "zz_koba.gen.go"),
			APIs:        []any{api},
		}},
	}
}
