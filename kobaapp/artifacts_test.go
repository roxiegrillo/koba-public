package kobaapp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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
