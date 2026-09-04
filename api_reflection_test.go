package koba

import (
	"reflect"
	"testing"
)

func TestDescribeAPIExcludesMetadataMethodsAndAcceptsTypedNil(t *testing.T) {
	var api *descriptionFixtureAPI

	description, err := DescribeAPI(api)
	if err != nil {
		t.Fatalf("DescribeAPI(typed nil) returned an error: %v", err)
	}
	if got, want := description.Name, "widgets"; got != want {
		t.Fatalf("description name = %q, want %q", got, want)
	}
	if got, want := len(description.Operations), 1; got != want {
		t.Fatalf("operation count = %d, want %d: %#v", got, want, description.Operations)
	}

	operation := description.Operations[0]
	if got, want := operation.MethodName, "Create"; got != want {
		t.Errorf("method name = %q, want %q", got, want)
	}
	if got, want := operation.OperationID, "createWidget"; got != want {
		t.Errorf("operation ID = %q, want %q", got, want)
	}
	if got, want := operation.Route, "/v1/widgets"; got != want {
		t.Errorf("route = %q, want %q", got, want)
	}
	if operation.MethodName == "Routes" || operation.MethodName == "APIDocumentation" {
		t.Fatalf("metadata method %q was exposed as an endpoint", operation.MethodName)
	}
	if got, want := operation.InputType, reflect.TypeFor[descriptionFixtureRequest](); got != want {
		t.Errorf("input type = %v, want %v", got, want)
	}
	if got, want := operation.OutputType, reflect.TypeFor[descriptionFixtureResponse](); got != want {
		t.Errorf("output type = %v, want %v", got, want)
	}
}

func TestDescribeAPIQualifiesDefaultOperationIDs(t *testing.T) {
	first, err := DescribeAPI(firstDefaultOperationAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI(first) returned an error: %v", err)
	}
	second, err := DescribeAPI(secondDefaultOperationAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI(second) returned an error: %v", err)
	}
	if first.Operations[0].OperationID == second.Operations[0].OperationID {
		t.Fatalf("default operation IDs collide: %q", first.Operations[0].OperationID)
	}
	if got, want := first.Operations[0].OperationID, "githubComRoxiegrilloKobaV2FirstDefaultOperationAPIGet"; got != want {
		t.Fatalf("first default operation ID = %q, want %q", got, want)
	}
}

func TestBusinessMethodNamedAPIDocumentationRemainsAnEndpoint(t *testing.T) {
	description, err := DescribeAPI(businessDocumentationMethodAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got, want := len(description.Operations), 1; got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}
	if got, want := description.Operations[0].MethodName, "APIDocumentation"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
}

func TestDescribeAPIPreservesExplicitEmptyRouteOverride(t *testing.T) {
	description, err := DescribeAPI(emptyRouteFixtureAPI{})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got := description.Operations[0].Route; got != "" {
		t.Fatalf("explicit route = %q, want empty", got)
	}
}
