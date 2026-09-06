package openapi

import (
	"net/http"
	"reflect"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

type projectedResponseItem struct {
	ID   string  `json:"id"`
	Note *string `json:"note,omitempty"`
}

type projectedResponseEnvelope struct {
	Items    []projectedResponseItem `json:"items"`
	Next     *string                 `json:"next,omitempty"`
	Count    int                     `json:"count"`
	Optional string                  `json:"optional,omitempty"`
}

func TestBuildProjectsResponseContractsAcrossNestedLists(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listItems",
		Route:       "/items",
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[projectedResponseEnvelope](),
		ResponseBindings: []fb.ResponseBinding{
			{
				FieldPath:  []string{"Items"},
				FieldIndex: []int{0},
				Input:      fb.InputOf[[]projectedResponseItem](fb.Required(), fb.MinItems(1), fb.MaxItems(100)),
				Documentation: &fb.BindingDocumentation{
					Description: "Current page of items.",
				},
			},
			{
				FieldPath:  []string{"Items"},
				FieldIndex: []int{0},
				ElementAt:  []int{1},
				Input:      fb.InputOf[projectedResponseItem](),
				Documentation: &fb.BindingDocumentation{
					Description: "One item in the current page.",
				},
			},
			{
				FieldPath:  []string{"Items", "ID"},
				FieldIndex: []int{0, 0},
				ElementAt:  []int{1},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)),
				Documentation: &fb.BindingDocumentation{
					Description: "Stable item identifier.",
					Deprecated:  true,
					Example:     "item-1",
				},
			},
			{
				FieldPath:  []string{"Items", "Note"},
				FieldIndex: []int{0, 1},
				ElementAt:  []int{1},
				Input:      fb.InputOf[*string](fb.Nullable()),
				Documentation: &fb.BindingDocumentation{
					Description: "Optional note.",
				},
			},
			{
				FieldPath:  []string{"Next"},
				FieldIndex: []int{1},
				Input:      fb.InputOf[*string](fb.Nullable()),
				Documentation: &fb.BindingDocumentation{
					Description: "Opaque continuation token.",
				},
			},
		},
	}, {
		MethodName:  "Raw",
		OperationID: "rawItems",
		Route:       "/raw-items",
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[projectedResponseEnvelope](),
	}}}

	customizerCalled := false
	document, err := Build(Config{
		Title:   "Items",
		Version: "1",
		SchemaCustomizer: func(context SchemaContext) error {
			if context.GoType == reflect.TypeFor[projectedResponseEnvelope]() {
				customizerCalled = true
			}
			return nil
		},
	}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	if !customizerCalled {
		t.Fatal("legacy SchemaCustomizer was not called")
	}

	media := document.Paths.Find("/items").Get.Responses.Value("200").Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("response schema = %#v, want operation-local schema", media)
	}
	root := media.Schema.Value
	if !containsString(root.Required, "items") || !containsString(root.Required, "count") {
		t.Errorf("response required fields = %v, want items and count", root.Required)
	}
	if containsString(root.Required, "next") || containsString(root.Required, "optional") {
		t.Errorf("omitempty response fields were required: %v", root.Required)
	}

	items := root.Properties["items"]
	if items == nil || items.Value == nil || items.Value.Description != "Current page of items." ||
		items.Value.MinItems != 1 || items.Value.MaxItems == nil || *items.Value.MaxItems != 100 {
		t.Fatalf("items schema = %#v", items)
	}
	item := resolvedSchema(items.Value.Items, document.Components.Schemas)
	if item == nil || !containsString(item.Required, "id") || containsString(item.Required, "note") {
		t.Fatalf("item schema required fields = %#v", item)
	}
	if item.Description != "One item in the current page." {
		t.Errorf("item schema description = %q", item.Description)
	}
	id := item.Properties["id"]
	if id == nil || id.Value == nil || id.Value.Description != "Stable item identifier." ||
		!id.Value.Deprecated || id.Value.Example != "item-1" || id.Value.MinLength != 1 ||
		id.Value.MaxLength == nil || *id.Value.MaxLength != 64 {
		t.Errorf("item ID schema = %#v", id)
	}
	note := item.Properties["note"]
	if note == nil || note.Value == nil || !note.Value.Nullable || note.Value.Description != "Optional note." {
		t.Errorf("item note schema = %#v", note)
	}
	next := root.Properties["next"]
	if next == nil || next.Value == nil || !next.Value.Nullable || next.Value.Description != "Opaque continuation token." {
		t.Errorf("next-token schema = %#v", next)
	}

	component := document.Components.Schemas[componentName(reflect.TypeFor[projectedResponseEnvelope]())]
	if component == nil || component.Value == nil {
		t.Fatal("original response component is missing")
	}
	if containsString(component.Value.Required, "items") || component.Value.Properties["items"].Value.Description != "" {
		t.Errorf("operation response contract mutated shared component: %#v", component.Value)
	}
}

func TestBuildPrunesReflectedComponentReplacedByOperationProjection(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listProjectedItems",
		Route:       "/projected-items",
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[projectedResponseEnvelope](),
		ResponseBindings: []fb.ResponseBinding{{
			FieldPath:  []string{"Items"},
			FieldIndex: []int{0},
			Input:      fb.InputOf[[]projectedResponseItem](fb.Required()),
		}},
	}}}
	document, err := Build(Config{Title: "Items", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	name := componentName(reflect.TypeFor[projectedResponseEnvelope]())
	if component := document.Components.Schemas[name]; component != nil {
		t.Fatalf("unreachable reflected component %q was retained", name)
	}
}

func TestBuildPreservesResponseComponentReferenceWithoutResponseContract(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Get",
		OperationID: "getItem",
		Route:       "/item",
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[projectedResponseItem](),
	}}}
	document, err := Build(Config{Title: "Items", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/item").Get.Responses.Value("200").Value.Content["application/json"]
	assertComponentReference(t, document, media.Schema, reflect.TypeFor[projectedResponseItem]())
}

func TestBuildProjectsRootScalarListResponseContract(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "ListLabels",
		OperationID: "listLabels",
		Route:       "/labels",
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[[]string](),
		ResponseBindings: []fb.ResponseBinding{{
			Input: fb.InputOf[[]string](
				fb.MinItems(1),
				fb.MaxItems(20),
				fb.UniqueItems(),
				fb.Elements(fb.Element[string](fb.WithFormat("slug")).Document("One label.")),
			),
			Documentation: &fb.BindingDocumentation{
				Description: "Labels assigned to the item.",
			},
		}},
	}}}
	document, err := Build(Config{Title: "Items", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/labels").Get.Responses.Value("200").Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("response schema = %#v", media)
	}
	schema := media.Schema.Value
	if schema.Description != "Labels assigned to the item." || schema.MinItems != 1 ||
		schema.MaxItems == nil || *schema.MaxItems != 20 || !schema.UniqueItems {
		t.Errorf("root list response schema = %#v", schema)
	}
	if schema.Items == nil || schema.Items.Value == nil || schema.Items.Value.Format != "slug" ||
		schema.Items.Value.Description != "One label." {
		t.Errorf("root list response item schema = %#v", schema.Items)
	}
}
