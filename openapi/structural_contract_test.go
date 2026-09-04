package openapi

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

type namedPageSize int

func TestBuildProjectsSemanticInputConstraintsThroughHTTPBindings(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listThings",
		Route:       "/things",
		HTTPMethod:  http.MethodGet,
		Contract: fb.OperationContract{Inputs: map[string]fb.InputContract{
			"pageSize":  fb.InputOf[int](fb.Default(20), fb.Min(1), fb.Max(100)),
			"pageToken": fb.InputOf[string](fb.MaxLength(2048)),
			"sequence":  fb.InputOf[int64](),
		}},
		Documentation: fb.OperationDocumentation{Parameters: []fb.ParameterDocumentation{
			{Name: "page_size", In: fb.ParameterInQuery, ContractInput: "pageSize", Description: "Page size"},
			{Name: "page_token", In: fb.ParameterInQuery, ContractInput: "pageToken", Description: "Page token"},
			{Name: "sequence", In: fb.ParameterInQuery, ContractInput: "sequence", Description: "Sequence"},
		}},
	}}}
	document, err := Build(Config{
		Title: "Things", Version: "1.0.0",
		SchemaCustomizer: func(context SchemaContext) error {
			if context.GoType == reflect.TypeFor[string]() {
				context.Schema.Format = "custom-string"
				context.Schema.Pattern = "^customized$"
			}
			return nil
		},
	}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/things").Get
	parameters := map[string]*openapi3.Parameter{}
	for _, reference := range operation.Parameters {
		parameters[reference.Value.Name] = reference.Value
	}
	pageSize := parameters["page_size"].Schema.Value
	if pageSize.Default != 20 || pageSize.Min == nil || *pageSize.Min != 1 || pageSize.Max == nil || *pageSize.Max != 100 {
		t.Fatalf("page_size schema = %#v", pageSize)
	}
	if parameters["page_size"].Required {
		t.Fatal("defaulted page_size was rendered as required")
	}
	pageToken := parameters["page_token"].Schema.Value
	if pageToken.MaxLength == nil || *pageToken.MaxLength != 2048 {
		t.Fatalf("page_token schema = %#v", pageToken)
	}
	if pageToken.Format != "custom-string" || pageToken.Pattern != "^customized$" {
		t.Fatalf("contract projection clobbered customized schema: %#v", pageToken)
	}
	if sequence := parameters["sequence"].Schema.Value; sequence.Format != "int64" {
		t.Fatalf("sequence format = %q, want int64", sequence.Format)
	}
	if response := operation.Responses.Value("400"); response == nil {
		t.Fatal("structurally validated HTTP parameters did not add a 400 response")
	}
}

func TestBuildProjectsNamedScalarParameterExampleAsJSONScalar(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listThings",
		Route:       "/things",
		HTTPMethod:  http.MethodGet,
		Contract: fb.OperationContract{Inputs: map[string]fb.InputContract{
			"pageSize": fb.InputOf[namedPageSize](fb.Min(1), fb.Max(100)),
		}},
		Documentation: fb.OperationDocumentation{Parameters: []fb.ParameterDocumentation{{
			Name: "page_size", In: fb.ParameterInQuery, ContractInput: "pageSize", Example: namedPageSize(42),
		}}},
	}}}
	document, err := Build(Config{Title: "Things", Version: "1.0.0"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	example := document.Paths.Find("/things").Get.Parameters[0].Value.Example
	if got, ok := example.(int64); !ok || got != 42 {
		t.Fatalf("example = %#v (%T), want int64(42)", example, example)
	}
}
