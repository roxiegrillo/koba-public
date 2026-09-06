package staticgen

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type documentedExampleDetails struct {
	Note string
}

type documentedStructExample struct {
	Name     string
	Details  documentedExampleDetails
	Optional *string
}

type documentedGenericBag[T any] []T

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

func TestAnyLiteralConvertsDocumentedStructExamplesToJSONValues(t *testing.T) {
	type example struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	literal, err := anyLiteral(example{Name: "fixture", Count: 2})
	if err != nil {
		t.Fatalf("anyLiteral(struct) returned an error: %v", err)
	}
	if got, want := literal, `map[string]any{"count":float64(2),"name":"fixture",}`; got != want {
		t.Fatalf("struct literal = %q, want %q", got, want)
	}
}

func TestDocumentationExampleLiteralPreservesCollectionElementType(t *testing.T) {
	imports := &importSet{paths: map[string]string{}}
	literal, err := documentationExampleLiteral(
		[]string{"framework", "sample"},
		reflect.TypeFor[*[]string](),
		imports,
	)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	if got, want := literal, `func() *[]string { var value []string = []string{"framework","sample",}; return &value }()`; got != want {
		t.Fatalf("collection literal = %q, want %q", got, want)
	}
}

func TestDocumentationExampleLiteralPreservesStructType(t *testing.T) {
	optional := "present"
	imports := &importSet{
		current: "github.com/roxiegrillo/koba/v2/internal/staticgen",
		paths:   map[string]string{},
	}
	literal, err := documentationExampleLiteral(
		documentedStructExample{
			Name:     "fixture",
			Details:  documentedExampleDetails{Note: "nested"},
			Optional: &optional,
		},
		reflect.TypeFor[documentedStructExample](),
		imports,
	)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	want := `documentedStructExample{Name:"fixture",Details:documentedExampleDetails{Note:"nested",},Optional:func() *string { var value string = "present"; return &value }(),}`
	if literal != want {
		t.Fatalf("struct literal = %q, want %q", literal, want)
	}
}

func TestDocumentationExampleLiteralTypesPointerToInterfaceValue(t *testing.T) {
	imports := &importSet{paths: map[string]string{}}
	literal, err := documentationExampleLiteral(
		"fallback",
		reflect.TypeFor[*any](),
		imports,
	)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	if got, want := literal, `func() *any { var value any = "fallback"; return &value }()`; got != want {
		t.Fatalf("pointer-to-interface literal = %q, want %q", got, want)
	}
}

func TestDocumentationExampleLiteralPreservesTypedNilCompositeValues(t *testing.T) {
	imports := &importSet{paths: map[string]string{}}
	for _, test := range []struct {
		name   string
		value  any
		target reflect.Type
		want   string
	}{
		{name: "pointer", value: (*string)(nil), target: reflect.TypeFor[*string](), want: "(*string)(nil)"},
		{name: "slice", value: []string(nil), target: reflect.TypeFor[[]string](), want: "[]string(nil)"},
		{name: "map", value: map[string]int(nil), target: reflect.TypeFor[map[string]int](), want: "map[string]int(nil)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			literal, err := documentationExampleLiteral(test.value, test.target, imports)
			if err != nil {
				t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
			}
			if literal != test.want {
				t.Fatalf("literal = %q, want %q", literal, test.want)
			}
		})
	}
}

func TestDocumentationExampleLiteralPreservesDynamicInterfaceTypes(t *testing.T) {
	value := &documentedStructExample{Name: "fixture"}
	imports := &importSet{
		current: "github.com/roxiegrillo/koba/v2/internal/staticgen",
		paths:   map[string]string{},
	}
	literal, err := documentationExampleLiteral(value, reflect.TypeFor[any](), imports)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	if !strings.Contains(literal, "*documentedStructExample") || !strings.Contains(literal, `Name:"fixture"`) {
		t.Fatalf("dynamic pointer literal = %q, want concrete pointer type and value", literal)
	}

	typedNil, err := documentationExampleLiteral((*documentedStructExample)(nil), reflect.TypeFor[any](), imports)
	if err != nil {
		t.Fatalf("typed nil literal returned an error: %v", err)
	}
	if typedNil != "(*documentedStructExample)(nil)" {
		t.Fatalf("typed nil literal = %q", typedNil)
	}
}

func TestDocumentationExampleLiteralSupportsAnonymousStruct(t *testing.T) {
	value := struct {
		Name string `json:"name"`
	}{Name: "fixture"}
	imports := &importSet{paths: map[string]string{}}
	literal, err := documentationExampleLiteral(value, reflect.TypeFor[any](), imports)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	if !strings.Contains(literal, `struct { Name string "json:\"name\""; }`) ||
		!strings.Contains(literal, `Name:"fixture"`) {
		t.Fatalf("anonymous struct literal = %q", literal)
	}
}

func TestTypeExpressionRejectsExternalUnexportedType(t *testing.T) {
	imports := &importSet{
		current: "github.com/roxiegrillo/koba/v2/internal/staticgen",
		paths:   map[string]string{},
	}
	_, err := documentationExampleLiteral(http.NoBody, reflect.TypeFor[any](), imports)
	if err == nil || !strings.Contains(err.Error(), "is not exported") {
		t.Fatalf("documentationExampleLiteral() error = %v, want external visibility error", err)
	}
}

func TestDocumentationExampleLiteralPreservesTime(t *testing.T) {
	value := time.Date(2026, time.September, 6, 12, 30, 0, 123, time.UTC)
	imports := &importSet{paths: map[string]string{}}
	literal, err := documentationExampleLiteral(value, reflect.TypeFor[time.Time](), imports)
	if err != nil {
		t.Fatalf("documentationExampleLiteral() returned an error: %v", err)
	}
	want := "time.Date(2026, 9, 6, 12, 30, 0, 123, time.UTC)"
	if literal != want {
		t.Fatalf("time literal = %q, want %q", literal, want)
	}
}

func TestDocumentationExampleLiteralRejectsInstantiatedGenericType(t *testing.T) {
	imports := &importSet{
		current: "github.com/roxiegrillo/koba/v2/internal/staticgen",
		paths:   map[string]string{},
	}
	_, err := documentationExampleLiteral(
		documentedGenericBag[string]{"value"},
		reflect.TypeFor[any](),
		imports,
	)
	if err == nil || !strings.Contains(err.Error(), "instantiated generic type") {
		t.Fatalf("documentationExampleLiteral() error = %v, want generic type rejection", err)
	}
}
