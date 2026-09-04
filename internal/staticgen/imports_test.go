package staticgen

import "testing"

func TestImportAliasesAvoidGeneratedAndLanguageIdentifiers(t *testing.T) {
	imports := &importSet{paths: map[string]string{
		"github.com/roxiegrillo/koba/v2": "fb",
	}}
	tests := map[string]string{
		"example.com/api":        "api2",
		"example.com/invocation": "invocation2",
		"example.com/type":       "type2",
		"example.com/string":     "string2",
	}
	for importPath, want := range tests {
		if got := imports.alias(importPath); got != want {
			t.Errorf("alias(%q) = %q, want %q", importPath, got, want)
		}
	}
}
