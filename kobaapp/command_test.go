package kobaapp

import (
	"os"
	"testing"
)

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
