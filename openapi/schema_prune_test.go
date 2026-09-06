package openapi

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

func TestPruneUnreachableSchemasTraversesRootsExtensionsAndTransitiveReferences(t *testing.T) {
	document := &openapi3.T{
		OpenAPI: Version,
		Info:    &openapi3.Info{Title: "Prune", Version: "1"},
		Paths:   openapi3.NewPaths(),
		Components: &openapi3.Components{Schemas: openapi3.Schemas{
			"Root":         {Value: openapi3.NewObjectSchema().WithPropertyRef("child", &openapi3.SchemaRef{Ref: "#/components/schemas/Child"})},
			"Child":        {Value: openapi3.NewObjectSchema().WithPropertyRef("root", &openapi3.SchemaRef{Ref: "#/components/schemas/Root"})},
			"SocketOnly":   {Value: openapi3.NewStringSchema()},
			"Escaped/Name": {Value: openapi3.NewStringSchema()},
			"Unused":       {Value: openapi3.NewStringSchema()},
		}},
	}
	operation := &openapi3.Operation{
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("ok").WithJSONSchemaRef(
				&openapi3.SchemaRef{Ref: "#/components/schemas/Root"},
			)}),
		),
		Extensions: map[string]any{
			"x-websocket": map[string]any{
				"clientToServer": map[string]any{"$ref": "#/components/schemas/SocketOnly"},
				"mapping":        "#/components/schemas/Escaped~1Name/properties/id",
			},
		},
	}
	document.Paths.Set("/root", &openapi3.PathItem{Get: operation})

	if err := pruneUnreachableSchemas(document); err != nil {
		t.Fatalf("pruneUnreachableSchemas() returned an error: %v", err)
	}
	for _, name := range []string{"Root", "Child", "SocketOnly", "Escaped/Name"} {
		if document.Components.Schemas[name] == nil {
			t.Errorf("reachable schema %q was pruned", name)
		}
	}
	if document.Components.Schemas["Unused"] != nil {
		t.Error("unreachable schema was retained")
	}
}

func TestBuildLeavesOnlyReachableSchemaComponents(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*documentedProtobufSocketAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Socket", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	encoded, err := document.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	loaded, err := openapi3.NewLoader().LoadFromData(encoded)
	if err != nil {
		t.Fatalf("reload generated document: %v", err)
	}
	if err := loaded.Validate(t.Context()); err != nil {
		t.Fatalf("validate reloaded document: %v", err)
	}
	for name := range document.Components.Schemas {
		if strings.TrimSpace(name) == "" {
			t.Fatal("generated document contains an empty schema component name")
		}
	}
}
