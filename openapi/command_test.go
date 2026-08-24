package openapi

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandWritesAndChecksGeneratedContract(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "nested", "openapi.json")
	options := CommandOptions{
		Config: Config{
			Title:   "Widget API",
			Version: "1",
			SecuritySchemes: []SecurityScheme{{
				Name:      "tokenAuth",
				Type:      SecurityTypeAPIKey,
				In:        "header",
				Parameter: "Authorization",
			}},
		},
		APIs: []any{(*contractFixtureAPI)(nil)},
	}

	writeCommand := NewCommand(options)
	writeCommand.SetArgs([]string{"--output", output})
	if err := writeCommand.Execute(); err != nil {
		t.Fatalf("write command returned an error: %v", err)
	}
	actual, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read generated contract: %v", err)
	}
	expected, err := GenerateJSON(options.Config, false, options.APIs...)
	if err != nil {
		t.Fatalf("GenerateJSON() returned an error: %v", err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("written contract differs from generated JSON")
	}
	if len(actual) == 0 || actual[len(actual)-1] != '\n' {
		t.Fatal("written contract does not end with a newline")
	}

	checkCommand := NewCommand(options)
	checkCommand.SetArgs([]string{"--check", "--output", output})
	if err := checkCommand.Execute(); err != nil {
		t.Fatalf("check command rejected a current contract: %v", err)
	}

	if err := os.WriteFile(output, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("make contract stale: %v", err)
	}
	staleCommand := NewCommand(options)
	staleCommand.SetArgs([]string{"--check", "--output", output})
	if err := staleCommand.Execute(); err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("stale check error = %v, want stale contract error", err)
	}
}

func TestCommandWritesToStdoutAndRejectsCheckingStdout(t *testing.T) {
	options := CommandOptions{Config: Config{Title: "Empty API", Version: "1"}}
	var output bytes.Buffer
	command := NewCommand(options)
	command.SetOut(&output)
	command.SetArgs([]string{"--output", "-", "--compact"})
	if err := command.Execute(); err != nil {
		t.Fatalf("stdout command returned an error: %v", err)
	}
	if got := output.String(); !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}\n") {
		t.Fatalf("stdout = %q, want compact JSON with trailing newline", got)
	}

	checkCommand := NewCommand(options)
	checkCommand.SetArgs([]string{"--check", "--output", "-"})
	if err := checkCommand.Execute(); err == nil || !strings.Contains(err.Error(), "requires a file output") {
		t.Fatalf("check stdout error = %v, want file-output error", err)
	}
}
