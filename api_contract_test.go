package koba

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestNormalizeOperationContractPreservesAbsentInputs(t *testing.T) {
	normalized, err := normalizeOperationContract(OperationContract{})
	if err != nil {
		t.Fatalf("normalizeOperationContract() returned an error: %v", err)
	}
	if normalized.Inputs != nil {
		t.Fatalf("normalized Inputs = %#v, want nil", normalized.Inputs)
	}
}

func TestValidateOperationInputsAppliesTypedDefaultsAndTracksPresence(t *testing.T) {
	contract, err := normalizeAPIContract(APIContract{Operations: map[string]OperationContract{
		"List": {Inputs: map[string]InputContract{
			"pageSize": fbInputForTest[int](Default(20), Min(1), Max(100)),
		}},
	}})
	if err != nil {
		t.Fatalf("normalizeAPIContract() returned an error: %v", err)
	}
	inputs, err := ValidateOperationInputs(contract.Operations["List"], nil)
	if err != nil {
		t.Fatalf("ValidateOperationInputs() returned an error: %v", err)
	}
	ctx := ContextWithOperationInputs(context.Background(), inputs)
	value, exists := InputValue[int](ctx, "pageSize")
	if !exists || value != 20 {
		t.Fatalf("InputValue(pageSize) = %d, %v; want 20, true", value, exists)
	}
	if InputWasProvided(ctx, "pageSize") {
		t.Fatal("defaulted pageSize was reported as provided")
	}

	provided, err := ValidateOperationInputs(contract.Operations["List"], map[string]any{"pageSize": 30})
	if err != nil {
		t.Fatal(err)
	}
	providedContext := ContextWithOperationInputs(context.Background(), provided)
	if !InputWasProvided(providedContext, "pageSize") {
		t.Fatal("explicit pageSize was not reported as provided")
	}
}

func TestNormalizeAPIContractPreservesAuthorizationAndCopiesScopes(t *testing.T) {
	scopes := []string{"read:items", "write:items"}
	contract, err := normalizeAPIContract(APIContract{Operations: map[string]OperationContract{
		"Update": {
			Authorization: AuthorizationContract{RequiredScopes: scopes},
		},
	}})
	if err != nil {
		t.Fatalf("normalizeAPIContract() returned an error: %v", err)
	}
	scopes[0] = "mutated"
	got := contract.Operations["Update"].Authorization.RequiredScopes
	if len(got) != 2 || got[0] != "read:items" || got[1] != "write:items" {
		t.Fatalf("normalized required scopes = %v", got)
	}
}

func TestNormalizeAPIContractRejectsInvalidAuthorizationScopes(t *testing.T) {
	for _, test := range []struct {
		name   string
		scopes []string
		want   string
	}{
		{name: "empty", scopes: []string{""}, want: "empty required scope"},
		{name: "whitespace", scopes: []string{"read:items write:items"}, want: "invalid required scope"},
		{name: "duplicate", scopes: []string{"read:items", "read:items"}, want: "duplicate required scope"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeAPIContract(APIContract{Operations: map[string]OperationContract{
				"Read": {Authorization: AuthorizationContract{RequiredScopes: test.scopes}},
			}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeAPIContract() error = %v, want %q", err, test.want)
			}
		})
	}
}

// Keeping this wrapper local makes failures identify whether options or
// normalization caused an unexpected type.
func fbInputForTest[T any](options ...InputOption) InputContract {
	return InputOf[T](options...)
}

func TestValidateOperationInputsUsesUnicodeCodePointsForLength(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"label": InputOf[string](MinLength(2), MaxLength(2)),
	}}
	if _, err := ValidateOperationInputs(contract, map[string]any{"label": "é😊"}); err != nil {
		t.Fatalf("two-code-point input failed validation: %v", err)
	}
	if _, err := ValidateOperationInputs(contract, map[string]any{"label": "é😊x"}); err == nil || !strings.Contains(err.Error(), "at most 2") {
		t.Fatalf("three-code-point validation error = %v, want max length", err)
	}
}

type contractLabelList []string

func TestValidateOperationInputsUsesSliceCardinality(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"labels": InputOf[contractLabelList](MinItems(1), MaxItems(2)),
	}}
	for _, value := range []contractLabelList{{"one"}, {"one", "two"}} {
		if _, err := ValidateOperationInputs(contract, map[string]any{"labels": value}); err != nil {
			t.Fatalf("valid labels %v failed validation: %v", value, err)
		}
	}
	for _, test := range []struct {
		value contractLabelList
		want  string
	}{
		{value: contractLabelList{}, want: "at least 1 items"},
		{value: contractLabelList{"one", "two", "three"}, want: "at most 2 items"},
	} {
		if _, err := ValidateOperationInputs(contract, map[string]any{"labels": test.value}); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Fatalf("labels %v error = %v, want %q", test.value, err, test.want)
		}
	}

	emptyOnly := OperationContract{Inputs: map[string]InputContract{
		"labels": InputOf[[]string](MaxItems(0)),
	}}
	if _, err := ValidateOperationInputs(emptyOnly, map[string]any{"labels": []string{}}); err != nil {
		t.Fatalf("MaxItems(0) rejected an empty slice: %v", err)
	}
	if _, err := ValidateOperationInputs(emptyOnly, map[string]any{"labels": []string{"one"}}); err == nil {
		t.Fatal("MaxItems(0) accepted a non-empty slice")
	}

	nullable := OperationContract{Inputs: map[string]InputContract{
		"labels": InputOf[[]string](Nullable(), MinItems(1)),
	}}
	if _, err := ValidateOperationInputs(nullable, map[string]any{"labels": nil}); err != nil {
		t.Fatalf("nullable collection rejected null: %v", err)
	}
}

func TestValidateOperationInputsUsesScalarCollectionElementContract(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"agentIDs": InputOf[[]string](
			MaxItems(3),
			UniqueItems(),
			Elements(Element[string](
				MinLength(2),
				Matches(`^[a-z]+$`),
				WithFormat("slug"),
			).Document("Stable agent identifier.", Example("agent"))),
		),
	}}

	inputs, err := ValidateOperationInputs(contract, map[string]any{
		"agentIDs": []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("valid collection failed validation: %v", err)
	}
	value, exists := inputs.Value("agentIDs")
	if !exists || !reflect.DeepEqual(value, []string{"alpha", "beta"}) {
		t.Fatalf("validated collection = %#v/%v", value, exists)
	}

	for _, test := range []struct {
		name  string
		value []string
		want  string
	}{
		{name: "duplicate", value: []string{"alpha", "alpha"}, want: "unique items"},
		{name: "short element", value: []string{"a"}, want: "item 1: length must be at least 2"},
		{name: "invalid element", value: []string{"UPPER"}, want: `item 1: must match "^[a-z]+$"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateOperationInputs(contract, map[string]any{"agentIDs": test.value})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateOperationInputs() error = %v, want %q", err, test.want)
			}
		})
	}

	normalized, err := normalizeInputContract(contract.Inputs["agentIDs"])
	if err != nil {
		t.Fatalf("normalizeInputContract() returned an error: %v", err)
	}
	if normalized.Items == nil || normalized.Items.Input.GoType != reflect.TypeFor[string]() ||
		normalized.Items.Input.Format != "slug" || normalized.Items.Documentation == nil ||
		normalized.Items.Documentation.Description != "Stable agent identifier." {
		t.Fatalf("normalized collection element = %#v", normalized.Items)
	}
}

type structuredCollectionDetails struct {
	Code string `json:"code"`
}

type structuredCollectionItem struct {
	Name    string                      `json:"name"`
	Kind    string                      `json:"kind"`
	Details structuredCollectionDetails `json:"details"`
}

type structuredCollectionDetailsContract struct {
	Code Binding[string]
}

type structuredCollectionItemContract struct {
	Name    Binding[string]
	Kind    Binding[string]
	Details structuredCollectionDetailsContract
}

func structuredItemsContract() InputContract {
	return InputOf[[]structuredCollectionItem](
		UniqueItems(),
		Elements(ElementObject[structuredCollectionItem](structuredCollectionItemContract{
			Name: Value[string](Required(), MinLength(2)).Document("Item tag name."),
			Kind: Value[string](Required(), OneOf("system", "user")).Document("Item tag kind."),
			Details: structuredCollectionDetailsContract{
				Code: Value[string](Required(), WithFormat("slug")).Document("Nested tag code."),
			},
		}).Document("Tag attached to an item.")),
	)
}

func TestValidateOperationInputsUsesStructuredCollectionElementContract(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{"tags": structuredItemsContract()}}
	valid := []structuredCollectionItem{{
		Name:    "sale",
		Kind:    "user",
		Details: structuredCollectionDetails{Code: "featured"},
	}}
	if _, err := ValidateOperationInputs(contract, map[string]any{"tags": valid}); err != nil {
		t.Fatalf("valid structured collection failed validation: %v", err)
	}

	invalid := append([]structuredCollectionItem(nil), valid...)
	invalid[0].Kind = "unknown"
	_, err := ValidateOperationInputs(contract, map[string]any{"tags": invalid})
	if err == nil || !strings.Contains(err.Error(), `item 1: field "Kind": must be one of`) {
		t.Fatalf("invalid structured collection error = %v", err)
	}

	normalized, err := normalizeInputContract(structuredItemsContract())
	if err != nil {
		t.Fatalf("normalizeInputContract() returned an error: %v", err)
	}
	if normalized.Items == nil || normalized.Items.Descriptor != nil || len(normalized.Items.Bindings) != 3 {
		t.Fatalf("normalized structured element = %#v", normalized.Items)
	}
	if got := normalized.Items.Bindings[2].FieldPath; !reflect.DeepEqual(got, []string{"Details", "Code"}) {
		t.Fatalf("nested binding path = %v", got)
	}
}

func TestCompositeDefaultsAreNormalizedAndClonedPerUse(t *testing.T) {
	attributes := map[string]any{
		"presentation": map[string]any{
			"labels": []any{"new", map[string]any{"priority": int64(1)}},
		},
	}
	tags := []string{}
	contract := OperationContract{Inputs: map[string]InputContract{
		"attributes": InputOf[map[string]any](Default(attributes)),
		"tags":       InputOf[[]string](Default(tags)),
	}}

	first, err := ValidateOperationInputs(contract, nil)
	if err != nil {
		t.Fatalf("first default application failed: %v", err)
	}
	firstAttributes, _ := first.Value("attributes")
	firstAttributes.(map[string]any)["presentation"].(map[string]any)["labels"].([]any)[0] = "mutated"
	firstTags, _ := first.Value("tags")
	firstTags = append(firstTags.([]string), "mutated")
	_ = firstTags

	second, err := ValidateOperationInputs(contract, nil)
	if err != nil {
		t.Fatalf("second default application failed: %v", err)
	}
	secondAttributes, _ := second.Value("attributes")
	labels := secondAttributes.(map[string]any)["presentation"].(map[string]any)["labels"].([]any)
	if labels[0] != "new" || labels[1].(map[string]any)["priority"] != int64(1) {
		t.Fatalf("second composite default aliases first use: %#v", secondAttributes)
	}
	secondTags, _ := second.Value("tags")
	if secondTags == nil || len(secondTags.([]string)) != 0 {
		t.Fatalf("empty slice default = %#v, want non-nil empty []string", secondTags)
	}
	if attributes["presentation"].(map[string]any)["labels"].([]any)[0] != "new" {
		t.Fatal("default normalization mutated the author-owned value")
	}
}

type namedStringPointer *string

func TestNamedPointerDefaultPreservesDeclaredType(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"value": InputOf[namedStringPointer](Default("fallback")),
	}}
	inputs, err := ValidateOperationInputs(contract, nil)
	if err != nil {
		t.Fatalf("ValidateOperationInputs() returned an error: %v", err)
	}
	value, exists := inputs.Value("value")
	typed, ok := value.(namedStringPointer)
	if !exists || !ok || typed == nil || *typed != "fallback" {
		t.Fatalf("named pointer default = %#v/%v, want fallback namedStringPointer", value, exists)
	}
}

func TestNormalizeInputContractRejectsRecursiveCollectionMetadata(t *testing.T) {
	item := &CollectionItemContract{}
	contract := InputContract{
		GoType: reflect.TypeFor[[]any](),
		Items:  item,
	}
	item.Input = contract
	_, err := normalizeInputContract(contract)
	if err == nil || !strings.Contains(err.Error(), "contains a cycle") {
		t.Fatalf("normalizeInputContract() error = %v, want collection cycle rejection", err)
	}
}

type recursiveCollectionNode struct {
	Children []recursiveCollectionNode `json:"children"`
}

func TestNormalizeInputContractRejectsCycleThroughCompiledElementBinding(t *testing.T) {
	items := &CollectionItemContract{Input: InputOf[recursiveCollectionNode]()}
	children := InputOf[[]recursiveCollectionNode]()
	children.Items = items
	items.Bindings = []ResponseBinding{{
		FieldPath:  []string{"Children"},
		FieldIndex: []int{0},
		Input:      children,
	}}
	root := InputOf[[]recursiveCollectionNode]()
	root.Items = items

	_, err := normalizeInputContract(root)
	if err == nil || !strings.Contains(err.Error(), "contains a cycle") {
		t.Fatalf("normalizeInputContract() error = %v, want compiled collection cycle rejection", err)
	}
}

type documentedCollectionExample struct {
	Name string `json:"name,omitempty"`
}

type documentedCollectionExampleContract struct {
	Name Binding[string]
}

func TestElementExampleHonorsNestedBindings(t *testing.T) {
	for _, test := range []struct {
		name    string
		example documentedCollectionExample
		options []InputOption
		want    string
	}{
		{
			name:    "constraint",
			example: documentedCollectionExample{Name: "x"},
			options: []InputOption{MinLength(3)},
			want:    "length must be at least 3",
		},
		{
			name:    "required presence",
			example: documentedCollectionExample{},
			options: []InputOption{Required()},
			want:    "is required",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract := InputOf[[]documentedCollectionExample](Elements(
				ElementObject[documentedCollectionExample](documentedCollectionExampleContract{
					Name: Value[string](test.options...),
				}).Document("Example element.", Example(test.example)),
			))
			_, err := normalizeInputContract(contract)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeInputContract() error = %v, want %q", err, test.want)
			}
		})
	}
}

type defaultedCollectionElement struct {
	Rank int `json:"rank,omitempty"`
}

type defaultedCollectionElementContract struct {
	Rank Binding[int]
}

func TestCollectionDefaultAppliesNestedElementDefaultsBeforeValidation(t *testing.T) {
	input := InputOf[[]defaultedCollectionElement](
		Default([]defaultedCollectionElement{{}}),
		Elements(ElementObject[defaultedCollectionElement](defaultedCollectionElementContract{
			Rank: Value[int](Default(7), Min(1)),
		})),
	)
	normalized, err := normalizeInputContract(input)
	if err != nil {
		t.Fatalf("normalizeInputContract() returned an error: %v", err)
	}
	got := normalized.Default.([]defaultedCollectionElement)
	if len(got) != 1 || got[0].Rank != 7 {
		t.Fatalf("materialized default = %#v, want rank 7", got)
	}
}

func TestCollectionElementExampleAppliesNestedDefaultsBeforeValidation(t *testing.T) {
	input := InputOf[[]defaultedCollectionElement](Elements(
		ElementObject[defaultedCollectionElement](defaultedCollectionElementContract{
			Rank: Value[int](Default(7), Min(1)),
		}).Document("Defaulted element.", Example(defaultedCollectionElement{})),
	))
	normalized, err := normalizeInputContract(input)
	if err != nil {
		t.Fatalf("normalizeInputContract() returned an error: %v", err)
	}
	got := normalized.Items.Documentation.Example.(defaultedCollectionElement)
	if got.Rank != 7 {
		t.Fatalf("materialized example = %#v, want rank 7", got)
	}
}

func TestValidateOperationInputsHonorsCollectionElementNullability(t *testing.T) {
	value := "present"
	for _, test := range []struct {
		name     string
		contract InputContract
		wantErr  string
	}{
		{
			name:     "non-nullable",
			contract: InputOf[[]*string](Elements(Element[*string]())),
			wantErr:  "item 2: must not be null",
		},
		{
			name:     "nullable",
			contract: InputOf[[]*string](Elements(Element[*string](Nullable()))),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateOperationInputs(
				OperationContract{Inputs: map[string]InputContract{"values": test.contract}},
				map[string]any{"values": []*string{&value, nil}},
			)
			if test.wantErr == "" && err != nil {
				t.Fatalf("nullable collection failed validation: %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("ValidateOperationInputs() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateOperationInputsRejectsNonFiniteCollectionElements(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := ValidateOperationInputs(
			OperationContract{Inputs: map[string]InputContract{
				"values": InputOf[[]float64](Elements(Element[float64]())),
			}},
			map[string]any{"values": []float64{value}},
		)
		if err == nil || !strings.Contains(err.Error(), "must be finite") {
			t.Fatalf("ValidateOperationInputs(%v) error = %v, want finite rejection", value, err)
		}
	}
}

func TestValidateOperationInputsPreservesExplicitNullableValues(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"value": InputOf[*string](Nullable()),
	}}
	inputs, err := ValidateOperationInputs(contract, map[string]any{"value": nil})
	if err != nil {
		t.Fatalf("ValidateOperationInputs() returned an error: %v", err)
	}
	value, exists := inputs.Value("value")
	typed, ok := value.(*string)
	if !exists || !ok || typed != nil || !inputs.WasProvided("value") {
		t.Fatalf("nullable value = %#v/%v/%v, want typed nil/present/provided", value, exists, inputs.WasProvided("value"))
	}

	_, err = ValidateOperationInputs(
		OperationContract{Inputs: map[string]InputContract{"value": InputOf[*string]()}},
		map[string]any{"value": nil},
	)
	if err == nil || !strings.Contains(err.Error(), "must not be null") {
		t.Fatalf("non-nullable input error = %v, want null rejection", err)
	}
}

func TestNormalizeInputContractRejectsContradictoryRules(t *testing.T) {
	tests := []struct {
		name      string
		input     InputContract
		wantError string
	}{
		{name: "untyped", input: InputContract{}, wantError: "Go type is required"},
		{name: "required default", input: InputOf[int](Required(), Default(1)), wantError: "required input cannot"},
		{name: "reversed numeric bounds", input: InputOf[int](Min(10), Max(1)), wantError: "minimum exceeds"},
		{name: "numeric length", input: InputOf[int](MaxLength(2)), wantError: "require a string"},
		{name: "numeric items", input: InputOf[int](MaxItems(2)), wantError: "require a JSON-array slice"},
		{name: "array items", input: InputOf[[2]string](MaxItems(2)), wantError: "require a JSON-array slice"},
		{name: "byte slice items", input: InputOf[[]byte](MaxItems(2)), wantError: "require a JSON-array slice"},
		{name: "reversed item bounds", input: InputOf[[]string](MinItems(2), MaxItems(1)), wantError: "minimum items 2 exceeds maximum items 1"},
		{name: "elements on scalar", input: InputOf[string](Elements(Element[string]())), wantError: "require a JSON-array slice"},
		{name: "element type mismatch", input: InputOf[[]string](Elements(Element[int]())), wantError: "collection element contract describes int"},
		{name: "map element contract", input: InputOf[[]map[string]string](Elements(Element[map[string]string]())), wantError: "requires a scalar or struct element"},
		{name: "bad default type", input: InputOf[int](Default("twenty")), wantError: "default"},
		{name: "null slice default", input: InputOf[[]string](Default([]string(nil))), wantError: "must not be null"},
		{name: "null map default", input: InputOf[map[string]string](Default(map[string]string(nil))), wantError: "must not be null"},
		{name: "default outside bounds", input: InputOf[int](Default(0), Min(1)), wantError: "default"},
		{name: "invalid regex", input: InputOf[string](Matches("[")), wantError: "invalid pattern"},
		{name: "non-finite default", input: InputOf[float64](Default(math.Inf(1))), wantError: "finite"},
		{name: "non-finite bound", input: InputOf[float64](Min(math.NaN())), wantError: "finite"},
		{name: "empty enum", input: InputOf[string](OneOf()), wantError: "at least one value"},
		{name: "nullable scalar", input: InputOf[string](Nullable()), wantError: "nullable input requires"},
		{name: "enum below minimum", input: InputOf[int](Min(1), OneOf(0, 1)), wantError: "enum value 1"},
		{name: "enum above maximum", input: InputOf[int](Max(10), OneOf(10, 11)), wantError: "enum value 2"},
		{name: "enum exceeds max length", input: InputOf[string](MaxLength(3), OneOf("okay")), wantError: "enum value 1"},
		{name: "enum violates pattern", input: InputOf[string](Matches(`^[a-z]+$`), OneOf("UPPER")), wantError: "enum value 1"},
		{name: "unsafe pointer is not numeric", input: InputContract{GoType: reflect.TypeFor[unsafe.Pointer](), Minimum: unsafe.Pointer(nil)}, wantError: "numeric Go type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeInputContract(test.input)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("normalizeInputContract() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestValidateOperationInputsReportsErrorsDeterministically(t *testing.T) {
	contract := OperationContract{Inputs: map[string]InputContract{
		"zeta":  InputOf[string](Required()),
		"alpha": InputOf[string](Required()),
	}}
	for attempt := 0; attempt < 50; attempt++ {
		_, err := ValidateOperationInputs(contract, nil)
		if err == nil || !strings.Contains(err.Error(), `input "alpha"`) {
			t.Fatalf("attempt %d error = %v, want alpha first", attempt, err)
		}
	}
}

func TestInputValuePanicsOnAccessorTypeMismatch(t *testing.T) {
	ctx := ContextWithOperationInputs(context.Background(), OperationInputs{values: map[string]operationInputValue{
		"count": {value: int64(3), provided: true},
	}})
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(recovered.(string), reflect.TypeFor[int64]().String()) {
			t.Fatalf("InputValue[int]() panic = %v, want type mismatch", recovered)
		}
	}()
	_, _ = InputValue[int](ctx, "count")
}
