package openapi

import (
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestSchemaForTypeRegistersTopLevelComponent(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := schemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[ContractFixtureRequest]())
	if err != nil {
		t.Fatalf("schemaForType() returned an error: %v", err)
	}
	t.Logf("ref=%#v schemas=%#v", ref, schemas)
	if ref.Ref == "" {
		t.Fatal("schemaForType() did not return a component reference")
	}
	name := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	if schemas[name] == nil {
		t.Fatalf("schemaForType() returned %q but did not register component %q", ref.Ref, name)
	}
}

func TestSchemaForTypeSupportsUnconstrainedInterfaces(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := schemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[any]())
	if err != nil {
		t.Fatalf("schemaForType(any) returned an error: %v", err)
	}
	if ref == nil || ref.Value == nil {
		t.Fatalf("schemaForType(any) = %#v, want an inline unconstrained schema", ref)
	}
	if ref.Ref != "" || ref.Value.Type != nil {
		t.Fatalf("schemaForType(any) = %#v, want an unconstrained schema", ref)
	}
}

func TestSchemaForTypePreservesPointerNullability(t *testing.T) {
	schemas := make(openapi3.Schemas)
	ref, err := responseSchemaForType(newSchemaGenerator(schemas, nil), schemas, reflect.TypeFor[*ContractFixtureResponse]())
	if err != nil {
		t.Fatalf("responseSchemaForType(*ContractFixtureResponse) returned an error: %v", err)
	}
	if ref == nil || ref.Value == nil || !ref.Value.Nullable {
		t.Fatalf("pointer schema = %#v, want nullable wrapper", ref)
	}
	if len(ref.Value.AllOf) != 1 || ref.Value.AllOf[0] == nil || ref.Value.AllOf[0].Ref == "" {
		t.Fatalf("pointer schema allOf = %#v, want one component reference", ref.Value.AllOf)
	}
}

func TestErrorResponseSchemaIsFullyDocumented(t *testing.T) {
	schema := errorResponseSchema()
	if schema.Description == "" {
		t.Fatal("ErrorResponse schema has no description")
	}
	for _, propertyName := range []string{"status", "error", "message"} {
		property := schema.Properties[propertyName]
		if property == nil || property.Value == nil || property.Value.Description == "" {
			t.Errorf("ErrorResponse.%s has no description", propertyName)
		}
	}
}

func TestGenerateDescribesKobaTimeAsDateTime(t *testing.T) {
	document, err := Generate(Config{Title: "Time API", Version: "1"}, (*contractTimeAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	operation := document.Paths.Find("/Current").Get
	response := operation.Responses.Value("200").Value.Content["application/json"]
	component := response.Schema.Value
	property := component.Properties["created_at"]
	if property == nil || property.Value == nil {
		t.Fatalf("created_at schema = %#v", property)
	}
	if got, want := property.Value.Format, "date-time"; got != want {
		t.Fatalf("created_at format = %q, want %q", got, want)
	}
}

func TestGenerateSupportsProjectSchemaCustomization(t *testing.T) {
	document, err := Generate(Config{
		Title:   "Custom API",
		Version: "1",
		SchemaCustomizer: func(context SchemaContext) error {
			if context.GoType == reflect.TypeFor[contractCustomID]() {
				*context.Schema = *openapi3.NewStringSchema()
				context.Schema.Format = "custom-id"
			}
			return nil
		},
	}, (*contractCustomAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	operation := document.Paths.Find("/Get").Get
	property := operation.Responses.Value("200").Value.Content["application/json"].Schema.Value.Properties["id"]
	if property == nil || property.Value == nil {
		t.Fatalf("custom ID schema = %#v", property)
	}
	if got, want := property.Value.Format, "custom-id"; got != want {
		t.Fatalf("custom ID format = %q, want %q", got, want)
	}
}

func TestGenerateSupportsEmptyStructResponses(t *testing.T) {
	document, err := Generate(Config{Title: "Empty API", Version: "1"}, (*contractEmptyAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	schema := document.Paths.Find("/Run").Get.Responses.Value("200").Value.Content["application/json"].Schema
	if schema == nil || schema.Value == nil || schema.Value.Type == nil || !schema.Value.Type.Is("object") {
		t.Fatalf("empty response schema = %#v, want object", schema)
	}
}
