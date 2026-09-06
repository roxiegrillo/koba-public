package openapi

import (
	"net/http"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

func TestWithDefaultProbeDescriptionsAddsFrameworkOperations(t *testing.T) {
	descriptions := withDefaultProbeDescriptions(nil)
	if len(descriptions) != 1 {
		t.Fatalf("description count = %d, want 1", len(descriptions))
	}
	if got, want := descriptions[0].Name, "system"; got != want {
		t.Errorf("API name = %q, want %q", got, want)
	}

	want := map[string]string{
		defaultHealthPath:    healthOperationID,
		defaultReadinessPath: readinessOperationID,
	}
	for _, operation := range descriptions[0].Operations {
		if got := want[operation.Route]; got != operation.OperationID {
			t.Errorf("GET %s operation ID = %q, want %q", operation.Route, operation.OperationID, got)
		}
		if operation.HTTPMethod != http.MethodGet {
			t.Errorf("%s HTTP method = %q, want GET", operation.Route, operation.HTTPMethod)
		}
		if !operation.Documentation.Public {
			t.Errorf("GET %s is not public", operation.Route)
		}
		delete(want, operation.Route)
	}
	if len(want) != 0 {
		t.Fatalf("missing probe descriptions: %v", want)
	}
}

func TestWithDefaultProbeDescriptionsIsIdempotent(t *testing.T) {
	once := withDefaultProbeDescriptions(nil)
	twice := withDefaultProbeDescriptions(once)
	if len(twice) != len(once) {
		t.Fatalf("second application produced %d descriptions, want %d", len(twice), len(once))
	}
}

func TestWithDefaultProbeDescriptionsPreservesExplicitRoute(t *testing.T) {
	explicit := fb.APIDescription{Name: "custom", Operations: []fb.OperationDescription{{
		MethodName:  "Health",
		OperationID: "custom_health",
		Route:       defaultHealthPath,
		HTTPMethod:  http.MethodGet,
	}}}
	originalOperations := len(explicit.Operations)
	descriptions := withDefaultProbeDescriptions([]fb.APIDescription{explicit})
	if len(descriptions) != 2 {
		t.Fatalf("description count = %d, want explicit API plus framework system API", len(descriptions))
	}
	if got := len(explicit.Operations); got != originalOperations {
		t.Fatalf("input description was mutated: operation count = %d, want %d", got, originalOperations)
	}

	counts := map[string]int{}
	operationIDs := map[string]string{}
	for _, description := range descriptions {
		for _, operation := range description.Operations {
			counts[operation.Route]++
			operationIDs[operation.Route] = operation.OperationID
		}
	}
	if counts[defaultHealthPath] != 1 || operationIDs[defaultHealthPath] != "custom_health" {
		t.Fatalf("explicit health description was not preserved: counts=%v operation IDs=%v", counts, operationIDs)
	}
	if counts[defaultReadinessPath] != 1 || operationIDs[defaultReadinessPath] != readinessOperationID {
		t.Fatalf("default readiness description was not added: counts=%v operation IDs=%v", counts, operationIDs)
	}
}
