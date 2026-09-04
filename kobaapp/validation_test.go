package kobaapp

import (
	"path/filepath"
	"strings"
	"testing"
)

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
