package koba

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

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
		{name: "bad default type", input: InputOf[int](Default("twenty")), wantError: "default"},
		{name: "default outside bounds", input: InputOf[int](Default(0), Min(1)), wantError: "default"},
		{name: "invalid regex", input: InputOf[string](Matches("[")), wantError: "invalid pattern"},
		{name: "non-finite default", input: InputOf[float64](Default(math.Inf(1))), wantError: "finite"},
		{name: "non-finite bound", input: InputOf[float64](Min(math.NaN())), wantError: "finite"},
		{name: "empty enum", input: InputOf[string](OneOf()), wantError: "at least one value"},
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
