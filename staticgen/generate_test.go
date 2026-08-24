package staticgen

import (
	"context"
	"reflect"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

type generatorRequest struct {
	Name string `json:"name"`
}

type generatorResponse struct {
	ID string `json:"id"`
}

type generatorFixtureAPI struct{}

type generatorValueAPI struct{}

func (generatorValueAPI) Ping() generatorResponse { return generatorResponse{} }

type generatorVoidAPI struct{}

func (generatorVoidAPI) Delete() {}

func (*generatorFixtureAPI) Create(context.Context, *generatorRequest) *generatorResponse {
	return &generatorResponse{}
}

func TestGeneratePreservesValueReceiverProvider(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{generatorValueAPI{}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if source := string(contents); !strings.Contains(source, "func (api generatorValueAPI) KobaCompiledAPI()") {
		t.Fatalf("generated provider does not preserve the value receiver:\n%s", source)
	}
}

func TestGenerateDoesNotImportReflectWhenContractHasNoTypes(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{generatorVoidAPI{}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if source := string(contents); strings.Contains(source, `reflect "reflect"`) {
		t.Fatalf("generated void-only provider contains an unused reflect import:\n%s", source)
	}
}

func (*generatorFixtureAPI) Get(context.Context) generatorResponse {
	return generatorResponse{}
}

func (*generatorFixtureAPI) Delete() {}

func (*generatorFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Name: "generator", Description: "Generator fixture.", Operations: map[string]fb.OperationDocumentation{
		"Create": {
			OperationID: "createGeneratorFixture",
			Summary:     "Create fixture",
			Tags:        []string{"fixtures"},
			Parameters: []fb.ParameterDocumentation{{
				Name: "mode", In: fb.ParameterInQuery, GoType: reflect.TypeFor[string](),
				Enum: []any{"safe", "fast"}, Example: "safe",
			}},
			Responses: map[int]fb.ResponseDocumentation{400: {Description: "Invalid fixture"}},
			Security:  []fb.SecurityRequirement{{"tokenAuth"}},
		},
		"Get":    {OperationID: "getGeneratorFixture"},
		"Delete": {OperationID: "deleteGeneratorFixture"},
	}}
}

func TestGenerateEmitsTypedInvokersBehindBuildTag(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorFixtureAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	checks := []string{
		"//go:build koba_static",
		"import (\n\treflect \"reflect\"\n\n\tfb \"github.com/roxiegrillo/koba/v2\"",
		"func (api *generatorFixtureAPI) KobaCompiledAPI() fb.CompiledAPI",
		`Description: fb.APIDescription{Name: "generator", Description: "Generator fixture."`,
		`InputType: reflect.TypeFor[*generatorRequest]()`,
		`OutputType: reflect.TypeFor[*generatorResponse]()`,
		`Enum: []any{"safe", "fast"}`,
		"var request generatorRequest",
		"invocation.Decode(&request)",
		"api.Create(invocation.Context, &request)",
		"if value == nil {",
		"Value: nil, HasValue: true",
		"Value: *value",
		"value := api.Get(invocation.Context)",
		"api.Delete()",
	}
	for _, check := range checks {
		if !strings.Contains(source, check) {
			t.Fatalf("generated source does not contain %q:\n%s", check, source)
		}
	}
	if strings.Contains(source, "DescribeReflectedAPI") {
		t.Fatalf("generated static plan still reflects its description at runtime:\n%s", source)
	}
}

func TestGenerateRejectsInvalidConfiguration(t *testing.T) {
	_, err := Generate(Config{})
	if err == nil || !strings.Contains(err.Error(), "package name is required") {
		t.Fatalf("Generate() error = %v, want missing-package error", err)
	}
	_, err = Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{nil},
	})
	if err == nil || !strings.Contains(err.Error(), "API 1 is nil") {
		t.Fatalf("Generate() error = %v, want nil-API error", err)
	}
}

func TestAnyLiteralPreservesFloat32Precision(t *testing.T) {
	literal, err := anyLiteral(float32(0.1))
	if err != nil {
		t.Fatalf("anyLiteral(float32) returned an error: %v", err)
	}
	if got, want := literal, "float32(0.1)"; got != want {
		t.Fatalf("float32 literal = %q, want %q", got, want)
	}
}

func TestAnyLiteralTypesLargeUnsignedInteger(t *testing.T) {
	literal, err := anyLiteral(uint64(^uint64(0)))
	if err != nil {
		t.Fatalf("anyLiteral(uint64) returned an error: %v", err)
	}
	if got, want := literal, "uint64(18446744073709551615)"; got != want {
		t.Fatalf("uint64 literal = %q, want %q", got, want)
	}
}

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
