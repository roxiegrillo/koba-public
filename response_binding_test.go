package koba

import (
	"reflect"
	"strings"
	"testing"
)

type responseBindingItem struct {
	ID   string  `json:"id"`
	Note *string `json:"note,omitempty"`
}

type responseBindingEnvelope struct {
	Items []*responseBindingItem `json:"items"`
	Next  *string                `json:"next,omitempty"`
	Count int                    `json:"count"`
}

type responseBindingItemContract struct {
	ID   ResponseValueBinding[string]
	Note ResponseValueBinding[*string]
}

type responseBindingEnvelopeContract struct {
	Items ResponseListBinding[*responseBindingItem]
	Next  ResponseValueBinding[*string]
}

type responseBindingAPI struct{}

func (*responseBindingAPI) List() responseBindingEnvelope { return responseBindingEnvelope{} }

func (*responseBindingAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"List": {Route: "/responses"},
	}}
}

func (*responseBindingAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"List": {Response: responseBindingEnvelopeContract{
			Items: ResponseList[*responseBindingItem](
				ResponseObject(responseBindingItemContract{
					ID: ResponseValue[string](Required(), MinLength(1), MaxLength(64)).Document(
						"Stable item identifier.", Example("item-1"),
					),
					Note: ResponseValue[*string](Nullable()).Document("Optional item note."),
				}, Nullable()).Document("One item in the current page."),
				Required(), MinItems(1), MaxItems(100),
			).Document("Current page of items."),
			Next: ResponseValue[*string](Nullable()).Document("Opaque continuation token."),
		}},
	}}
}

func TestDescribeReflectedAPICompilesNestedResponseBindingsAndDocumentedRoute(t *testing.T) {
	description, err := DescribeReflectedAPI((*responseBindingAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	operation := description.Operations[0]
	if got, want := operation.Route, "/responses"; got != want {
		t.Fatalf("route = %q, want %q", got, want)
	}
	if operation.Contract.Response != nil {
		t.Fatalf("compiled contract retained response companion: %#v", operation.Contract.Response)
	}
	if got, want := len(operation.ResponseBindings), 5; got != want {
		t.Fatalf("response binding count = %d, want %d: %#v", got, want, operation.ResponseBindings)
	}

	items := operation.ResponseBindings[0]
	if !reflect.DeepEqual(items.FieldPath, []string{"Items"}) ||
		items.Input.GoType != reflect.TypeFor[[]*responseBindingItem]() ||
		!items.Input.Required || items.Input.MinItems == nil || *items.Input.MinItems != 1 ||
		items.Input.MaxItems == nil || *items.Input.MaxItems != 100 {
		t.Fatalf("items response binding = %#v", items)
	}
	element := operation.ResponseBindings[1]
	if !reflect.DeepEqual(element.FieldPath, []string{"Items"}) ||
		!reflect.DeepEqual(element.ElementAt, []int{1}) ||
		element.Input.GoType != reflect.TypeFor[*responseBindingItem]() || !element.Input.Nullable ||
		element.Documentation == nil || element.Documentation.Description != "One item in the current page." {
		t.Fatalf("item element binding = %#v", element)
	}
	id := operation.ResponseBindings[2]
	if !reflect.DeepEqual(id.FieldPath, []string{"Items", "ID"}) ||
		!reflect.DeepEqual(id.FieldIndex, []int{0, 0}) ||
		!reflect.DeepEqual(id.ElementAt, []int{1}) ||
		id.Documentation == nil || id.Documentation.Example != "item-1" {
		t.Fatalf("nested item ID binding = %#v", id)
	}
	note := operation.ResponseBindings[3]
	if !reflect.DeepEqual(note.FieldPath, []string{"Items", "Note"}) || !note.Input.Nullable {
		t.Fatalf("nested item note binding = %#v", note)
	}
	next := operation.ResponseBindings[4]
	if !reflect.DeepEqual(next.FieldPath, []string{"Next"}) || next.Documentation == nil {
		t.Fatalf("next-token binding = %#v", next)
	}
}

type duplicateDocumentedRouteAPI struct{}

func (*duplicateDocumentedRouteAPI) Get() {}

func (*duplicateDocumentedRouteAPI) Routes() map[string]string {
	return map[string]string{"Get": "/legacy"}
}

func (*duplicateDocumentedRouteAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"Get": {Route: "/documented"},
	}}
}

func TestDescribeReflectedAPIRejectsRouteDeclaredTwice(t *testing.T) {
	_, err := DescribeReflectedAPI((*duplicateDocumentedRouteAPI)(nil))
	if err == nil || !strings.Contains(err.Error(), "route is declared in both Routes and APIDocumentation") {
		t.Fatalf("DescribeReflectedAPI() error = %v, want duplicate-route error", err)
	}
}

type responseBindingMismatchAPI struct{}

func (*responseBindingMismatchAPI) Get() responseBindingEnvelope { return responseBindingEnvelope{} }

func (*responseBindingMismatchAPI) APIContract() APIContract {
	type mismatch struct {
		Count ResponseValueBinding[string]
	}
	return APIContract{Operations: map[string]OperationContract{
		"Get": {Response: mismatch{Count: ResponseValue[string]()}},
	}}
}

func TestDescribeReflectedAPIRejectsResponseBindingTypeMismatch(t *testing.T) {
	_, err := DescribeReflectedAPI((*responseBindingMismatchAPI)(nil))
	if err == nil || !strings.Contains(err.Error(), "describes string, but response field has type int") {
		t.Fatalf("DescribeReflectedAPI() error = %v, want response type mismatch", err)
	}
}

type responseBindingVoidAPI struct{}

func (*responseBindingVoidAPI) Delete() {}

func (*responseBindingVoidAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Delete": {Response: ResponseValue[string]()},
	}}
}

func TestDescribeReflectedAPIRejectsResponseContractOnVoidOperation(t *testing.T) {
	_, err := DescribeReflectedAPI((*responseBindingVoidAPI)(nil))
	if err == nil || !strings.Contains(err.Error(), "response contract is declared but the operation has no response value") {
		t.Fatalf("DescribeReflectedAPI() error = %v, want void-response error", err)
	}
}

type scalarResponseBindingAPI struct{}

func (*scalarResponseBindingAPI) Get() string { return "ok" }

func (*scalarResponseBindingAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Get": {Response: ResponseValue[string](MinLength(1)).Document("Result value.", Example("ok"))},
	}}
}

func TestDescribeReflectedAPICompilesRootScalarResponse(t *testing.T) {
	description, err := DescribeReflectedAPI((*scalarResponseBindingAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	bindings := description.Operations[0].ResponseBindings
	if len(bindings) != 1 || len(bindings[0].FieldPath) != 0 ||
		bindings[0].Input.GoType != reflect.TypeFor[string]() || bindings[0].Input.MinLength == nil ||
		bindings[0].Documentation == nil || bindings[0].Documentation.Example != "ok" {
		t.Fatalf("root scalar response binding = %#v", bindings)
	}
}

func TestCompositeResponseDocumentationExampleIsOwned(t *testing.T) {
	example := map[string]any{"labels": []any{"original"}}
	documentation, err := normalizeResponseDocumentation(
		&BindingDocumentation{Description: "Composite response.", Example: example},
		InputOf[map[string]any](),
	)
	if err != nil {
		t.Fatalf("normalizeResponseDocumentation() returned an error: %v", err)
	}
	example["labels"].([]any)[0] = "mutated"
	got := documentation.Example.(map[string]any)["labels"].([]any)[0]
	if got != "original" {
		t.Fatalf("compiled response example aliases author value: %v", got)
	}
}

type scalarListResponseBindingAPI struct{}

func (*scalarListResponseBindingAPI) List() []string { return nil }

func (*scalarListResponseBindingAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"List": {Response: ResponseList[string](nil,
			MinItems(1),
			MaxItems(20),
			UniqueItems(),
			Elements(Element[string](WithFormat("slug")).Document("One label.")),
		).Document("Labels.")},
	}}
}

func TestDescribeReflectedAPICompilesRootScalarListResponse(t *testing.T) {
	description, err := DescribeReflectedAPI((*scalarListResponseBindingAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	bindings := description.Operations[0].ResponseBindings
	if len(bindings) != 1 || len(bindings[0].FieldPath) != 0 ||
		bindings[0].Input.GoType != reflect.TypeFor[[]string]() || bindings[0].Input.MinItems == nil ||
		!bindings[0].Input.UniqueItems || bindings[0].Input.Items == nil ||
		bindings[0].Input.Items.Input.Format != "slug" || bindings[0].Input.Items.Documentation == nil ||
		bindings[0].Documentation == nil || bindings[0].Documentation.Description != "Labels." {
		t.Fatalf("root list response binding = %#v", bindings)
	}
}

func TestNormalizeResolvedResponseBindingsRejectsMalformedGeneratedPath(t *testing.T) {
	_, err := normalizeResolvedResponseBindings([]ResponseBinding{{
		FieldPath:  []string{"Count"},
		FieldIndex: []int{2},
		ElementAt:  []int{1},
		Input:      InputOf[int](),
	}}, reflect.TypeFor[responseBindingEnvelope]())
	if err == nil || !strings.Contains(err.Error(), "targets non-list type int") {
		t.Fatalf("normalizeResolvedResponseBindings() error = %v, want malformed element offset", err)
	}
}
