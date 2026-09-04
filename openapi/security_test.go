package openapi

import "testing"

func TestRenderSecurityRequirementsPreservesAndOrSemantics(t *testing.T) {
	known := map[string]struct{}{"token": {}, "signature": {}, "fallback": {}}
	requirements, err := renderSecurityRequirements([]SecurityRequirement{
		{"token", "signature"},
		{"fallback"},
		{},
	}, known)
	if err != nil {
		t.Fatalf("renderSecurityRequirements() returned an error: %v", err)
	}
	if got, want := len(requirements), 3; got != want {
		t.Fatalf("alternative count = %d, want %d", got, want)
	}
	if _, exists := requirements[0]["token"]; !exists {
		t.Fatal("first alternative does not require token")
	}
	if _, exists := requirements[0]["signature"]; !exists {
		t.Fatal("first alternative does not require signature")
	}
	if _, exists := requirements[1]["fallback"]; !exists {
		t.Fatal("second alternative does not require fallback")
	}
	if got := len(requirements[2]); got != 0 {
		t.Fatalf("anonymous alternative has %d schemes, want none", got)
	}
}
