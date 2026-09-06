package koba

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	internalvaluekind "github.com/roxiegrillo/koba/v2/internal/valuekind"
)

// InputValidationError identifies a structural input rejected before an API
// function was invoked.
type InputValidationError struct {
	Input string
	Cause error
}

func (err *InputValidationError) Error() string {
	return fmt.Sprintf("input %q: %v", err.Input, err.Cause)
}

func (err *InputValidationError) Unwrap() error { return err.Cause }

func normalizeAPIContract(contract APIContract) (APIContract, error) {
	result := APIContract{Operations: make(map[string]OperationContract, len(contract.Operations))}
	for _, methodName := range sortedMapKeys(contract.Operations) {
		operation := contract.Operations[methodName]
		if strings.TrimSpace(methodName) == "" {
			return APIContract{}, fmt.Errorf("koba: API contract contains an empty operation name")
		}
		normalized, err := normalizeOperationContract(operation)
		if err != nil {
			return APIContract{}, fmt.Errorf("koba: %s: %w", methodName, err)
		}
		result.Operations[methodName] = normalized
	}
	return result, nil
}

func normalizeOperationContract(operation OperationContract) (OperationContract, error) {
	var inputs map[string]InputContract
	if operation.Inputs != nil {
		inputs = make(map[string]InputContract, len(operation.Inputs))
	}
	normalized := OperationContract{
		Inputs:   inputs,
		Request:  operation.Request,
		Response: operation.Response,
		Authorization: AuthorizationContract{
			RequiredScopes: append([]string(nil), operation.Authorization.RequiredScopes...),
		},
	}
	transport, err := normalizeTransportContract(operation.Transport)
	if err != nil {
		return OperationContract{}, fmt.Errorf("transport: %w", err)
	}
	normalized.Transport = transport
	if err := validateRequiredScopes(normalized.Authorization.RequiredScopes); err != nil {
		return OperationContract{}, fmt.Errorf("authorization: %w", err)
	}
	for _, identifier := range sortedMapKeys(operation.Inputs) {
		input := operation.Inputs[identifier]
		if strings.TrimSpace(identifier) == "" {
			return OperationContract{}, fmt.Errorf("contract contains an empty input identifier")
		}
		value, err := normalizeInputContract(input)
		if err != nil {
			return OperationContract{}, fmt.Errorf("input %s: %w", identifier, err)
		}
		normalized.Inputs[identifier] = value
	}
	return normalized, nil
}

func normalizeInputContract(input InputContract) (InputContract, error) {
	return normalizeInputContractWithState(input, make(map[*CollectionItemContract]struct{}))
}

func normalizeInputContractWithState(
	input InputContract,
	activeItems map[*CollectionItemContract]struct{},
) (InputContract, error) {
	if input.GoType == nil {
		return InputContract{}, fmt.Errorf("Go type is required")
	}
	result := input
	if input.Enum != nil {
		result.Enum = append([]any{}, input.Enum...)
	}
	if input.MinLength != nil {
		value := *input.MinLength
		result.MinLength = &value
	}
	if input.MaxLength != nil {
		value := *input.MaxLength
		result.MaxLength = &value
	}
	if input.MinItems != nil {
		value := *input.MinItems
		result.MinItems = &value
	}
	if input.MaxItems != nil {
		value := *input.MaxItems
		result.MaxItems = &value
	}
	if input.Items != nil {
		if _, active := activeItems[input.Items]; active {
			return InputContract{}, fmt.Errorf("collection element contract contains a cycle")
		}
		activeItems[input.Items] = struct{}{}
		defer delete(activeItems, input.Items)
		items := *input.Items
		items.Documentation = cloneBindingDocumentation(input.Items.Documentation)
		result.Items = &items
	}

	valueType := input.GoType
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	kind := valueType.Kind()
	if input.Nullable && !nilCapableType(input.GoType) {
		return InputContract{}, fmt.Errorf("nullable input requires a pointer, map, or slice Go type, got %s", input.GoType)
	}
	if input.Enum != nil && !contractLiteralKind(kind) {
		return InputContract{}, fmt.Errorf("enum requires a scalar Go type, got %s", input.GoType)
	}
	if input.HasDefault && !contractDefaultType(input.GoType) {
		return InputContract{}, fmt.Errorf(
			"default requires a deterministic scalar, struct, array, slice, or string-keyed map Go type, got %s",
			input.GoType,
		)
	}
	if (input.Minimum != nil || input.Maximum != nil) && !numericKind(kind) {
		return InputContract{}, fmt.Errorf("minimum and maximum require a numeric Go type, got %s", input.GoType)
	}
	if (input.MinLength != nil || input.MaxLength != nil || input.Pattern != "") && kind != reflect.String {
		return InputContract{}, fmt.Errorf("length and pattern constraints require a string Go type, got %s", input.GoType)
	}
	if input.MinLength != nil && input.MaxLength != nil && *input.MinLength > *input.MaxLength {
		return InputContract{}, fmt.Errorf("minimum length %d exceeds maximum length %d", *input.MinLength, *input.MaxLength)
	}
	if (input.MinItems != nil || input.MaxItems != nil || input.UniqueItems || input.Items != nil) &&
		(kind != reflect.Slice || valueType.Elem().Kind() == reflect.Uint8) {
		return InputContract{}, fmt.Errorf("item constraints require a JSON-array slice Go type, got %s", input.GoType)
	}
	if input.MinItems != nil && input.MaxItems != nil && *input.MinItems > *input.MaxItems {
		return InputContract{}, fmt.Errorf("minimum items %d exceeds maximum items %d", *input.MinItems, *input.MaxItems)
	}
	if input.Items != nil {
		elementType := valueType.Elem()
		elementContract, err := normalizeInputContractWithState(input.Items.Input, activeItems)
		if err != nil {
			return InputContract{}, fmt.Errorf("collection element: %w", err)
		}
		if elementContract.GoType != elementType {
			return InputContract{}, fmt.Errorf(
				"collection element contract describes %s, but collection contains %s",
				elementContract.GoType,
				elementType,
			)
		}
		elementValueType := elementType
		for elementValueType.Kind() == reflect.Pointer {
			elementValueType = elementValueType.Elem()
		}
		if !contractLiteralKind(elementValueType.Kind()) && elementValueType.Kind() != reflect.Struct {
			return InputContract{}, fmt.Errorf(
				"collection element contract requires a scalar or struct element Go type, got %s",
				elementType,
			)
		}
		documentation, err := normalizeResponseDocumentation(input.Items.Documentation, elementContract)
		if err != nil {
			return InputContract{}, fmt.Errorf("collection element: %w", err)
		}
		if input.Items.Descriptor != nil && input.Items.Bindings != nil {
			return InputContract{}, fmt.Errorf(
				"collection element cannot contain both an authoring descriptor and compiled bindings",
			)
		}
		var bindings []ResponseBinding
		switch {
		case input.Items.Descriptor != nil:
			if elementValueType.Kind() != reflect.Struct {
				return InputContract{}, fmt.Errorf(
					"collection element descriptor requires a struct element Go type, got %s",
					elementType,
				)
			}
			bindings, err = normalizeCollectionElementBindings(input.Items.Descriptor, elementType)
		case input.Items.Bindings != nil:
			bindings, err = normalizeResolvedResponseBindingsWithState(
				input.Items.Bindings,
				elementType,
				activeItems,
			)
		}
		if err != nil {
			return InputContract{}, fmt.Errorf("collection element: %w", err)
		}
		result.Items = &CollectionItemContract{
			Input:         elementContract,
			Documentation: documentation,
			Bindings:      bindings,
		}
		if documentation != nil && documentation.Example != nil {
			example, err := normalizeCollectionElementExample(result.Items, documentation.Example)
			if err != nil {
				return InputContract{}, fmt.Errorf("collection element example: %w", err)
			}
			result.Items.Documentation.Example = example
		}
	}
	if input.Pattern != "" {
		if _, err := regexp.Compile(input.Pattern); err != nil {
			return InputContract{}, fmt.Errorf("invalid pattern: %w", err)
		}
	}
	if input.Required && input.HasDefault {
		return InputContract{}, fmt.Errorf("required input cannot also have a default")
	}
	if input.Enum != nil && len(input.Enum) == 0 {
		return InputContract{}, fmt.Errorf("enum must contain at least one value")
	}

	var err error
	if input.Minimum != nil {
		result.Minimum, err = normalizeContractValue(input.Minimum, valueType)
		if err != nil {
			return InputContract{}, fmt.Errorf("minimum: %w", err)
		}
	}
	if input.Maximum != nil {
		result.Maximum, err = normalizeContractValue(input.Maximum, valueType)
		if err != nil {
			return InputContract{}, fmt.Errorf("maximum: %w", err)
		}
	}
	if result.Minimum != nil && result.Maximum != nil && compareContractValues(result.Minimum, result.Maximum) > 0 {
		return InputContract{}, fmt.Errorf("minimum exceeds maximum")
	}
	for index, value := range input.Enum {
		result.Enum[index], err = normalizeContractValue(value, valueType)
		if err != nil {
			return InputContract{}, fmt.Errorf("enum value %d: %w", index+1, err)
		}
		for previous := 0; previous < index; previous++ {
			if reflect.DeepEqual(result.Enum[previous], result.Enum[index]) {
				return InputContract{}, fmt.Errorf("enum contains duplicate value %v", result.Enum[index])
			}
		}
	}
	for index, value := range result.Enum {
		if err := validateInputValue(result, value); err != nil {
			return InputContract{}, fmt.Errorf("enum value %d: %w", index+1, err)
		}
	}
	if input.HasDefault {
		result.Default, err = normalizeContractValue(input.Default, input.GoType)
		if err != nil {
			return InputContract{}, fmt.Errorf("default: %w", err)
		}
		if isNilContractValue(result.Default) {
			return InputContract{}, fmt.Errorf("default: must not be null")
		}
		result.Default, err = cloneDeterministicContractValue(result.Default)
		if err != nil {
			return InputContract{}, fmt.Errorf("default: %w", err)
		}
		var document any
		if result.Items != nil {
			result.Default, document, err = materializeCollectionContractValue(result, result.Default)
			if err != nil {
				return InputContract{}, fmt.Errorf("default: %w", err)
			}
		}
		if err := validateInputValue(result, result.Default); err != nil {
			return InputContract{}, fmt.Errorf("default: %w", err)
		}
		if document != nil {
			if err := validateCollectionInputDocument(result, document); err != nil {
				return InputContract{}, fmt.Errorf("default: %w", err)
			}
		}
	}
	return result, nil
}

func materializeCollectionContractValue(input InputContract, value any) (any, any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot be represented as JSON: %w", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, nil, fmt.Errorf("JSON cannot be inspected: %w", err)
	}
	target := reflect.New(input.GoType).Elem()
	source := reflect.ValueOf(value)
	if !source.IsValid() || !source.Type().AssignableTo(input.GoType) {
		return nil, nil, fmt.Errorf("value has type %T, want %s", value, input.GoType)
	}
	target.Set(source)
	if err := applyCollectionInputDefaults(input, document, target); err != nil {
		return nil, nil, err
	}
	return target.Interface(), document, nil
}

func normalizeCollectionElementExample(items *CollectionItemContract, example any) (any, error) {
	collectionType := reflect.SliceOf(items.Input.GoType)
	collection := reflect.MakeSlice(collectionType, 1, 1)
	value := reflect.ValueOf(example)
	if !value.IsValid() || !value.Type().AssignableTo(items.Input.GoType) {
		return nil, fmt.Errorf("value has type %T, want %s", example, items.Input.GoType)
	}
	collection.Index(0).Set(value)
	input := InputContract{GoType: collectionType, Items: items}
	materialized, document, err := materializeCollectionContractValue(input, collection.Interface())
	if err != nil {
		return nil, err
	}
	if err := validateInputValue(input, materialized); err != nil {
		return nil, err
	}
	if err := validateCollectionInputDocument(input, document); err != nil {
		return nil, err
	}
	return reflect.ValueOf(materialized).Index(0).Interface(), nil
}

func normalizeCollectionElementBindings(descriptor any, elementType reflect.Type) ([]ResponseBinding, error) {
	walker := requestBindingWalker{
		activePointers: make(map[uintptr]string),
		wireBindings:   make(map[string]string),
	}
	if err := walker.walk(reflect.ValueOf(descriptor), elementType, nil, nil, true); err != nil {
		return nil, err
	}
	if len(walker.bindings) == 0 {
		return nil, fmt.Errorf("object descriptor contains no value bindings")
	}
	bindings := make([]ResponseBinding, 0, len(walker.bindings))
	for _, binding := range walker.bindings {
		if binding.Source != BindingSourceValue {
			return nil, fmt.Errorf(
				"object descriptor field %q uses %s; collection elements only accept Value bindings",
				displayRequestFieldPath(binding.FieldPath),
				binding.Source,
			)
		}
		bindings = append(bindings, ResponseBinding{
			FieldPath:     append([]string(nil), binding.FieldPath...),
			FieldIndex:    append([]int(nil), binding.FieldIndex...),
			Input:         binding.Input,
			Documentation: cloneBindingDocumentation(binding.Documentation),
		})
	}
	return bindings, nil
}

func contractDefaultType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	switch valueType.Kind() {
	case reflect.Interface, reflect.Struct, reflect.Array, reflect.Slice:
		return true
	case reflect.Map:
		return valueType.Key().Kind() == reflect.String
	default:
		return contractLiteralKind(valueType.Kind())
	}
}

// cloneDeterministicContractValue validates the portable Go value shape while
// producing an ownership-independent value. Defaults are cloned again when
// applied so a function cannot mutate framework metadata for later requests.
func cloneDeterministicContractValue(value any) (any, error) {
	return cloneContractValue(value, false)
}

// cloneDocumentationContractValue owns mutable JSON-visible example data while
// preserving opaque private state in domain structs. Private fields cannot be
// reconstructed safely outside their package, and examples treated them as
// value-semantic before composite metadata was introduced.
func cloneDocumentationContractValue(value any) (any, error) {
	return cloneContractValue(value, true)
}

func cloneContractValue(value any, preservePrivateFields bool) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("value cannot be nil")
	}
	cloned, err := cloneDeterministicValue(
		reflect.ValueOf(value),
		make(map[contractValueVisit]struct{}),
		preservePrivateFields,
	)
	if err != nil {
		return nil, err
	}
	return cloned.Interface(), nil
}

type contractValueVisit struct {
	typeOf  reflect.Type
	pointer uintptr
}

func cloneDeterministicValue(
	value reflect.Value,
	active map[contractValueVisit]struct{},
	preservePrivateFields bool,
) (reflect.Value, error) {
	if !value.IsValid() {
		return reflect.Value{}, fmt.Errorf("invalid value")
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		child, err := cloneDeterministicValue(value.Elem(), active, preservePrivateFields)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(child)
		return result, nil
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		visit := contractValueVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if _, cyclic := active[visit]; cyclic {
			return reflect.Value{}, fmt.Errorf("value contains a pointer cycle")
		}
		active[visit] = struct{}{}
		defer delete(active, visit)
		child, err := cloneDeterministicValue(value.Elem(), active, preservePrivateFields)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(child)
		if result.Type() == value.Type() {
			return result, nil
		}
		if result.Type().ConvertibleTo(value.Type()) {
			return result.Convert(value.Type()), nil
		}
		return reflect.Value{}, fmt.Errorf("pointer type %s cannot be reconstructed", value.Type())
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, fmt.Errorf("map type %s must use string keys", value.Type())
		}
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		visit := contractValueVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if _, cyclic := active[visit]; cyclic {
			return reflect.Value{}, fmt.Errorf("value contains a map cycle")
		}
		active[visit] = struct{}{}
		defer delete(active, visit)
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			child, err := cloneDeterministicValue(iterator.Value(), active, preservePrivateFields)
			if err != nil {
				return reflect.Value{}, err
			}
			result.SetMapIndex(iterator.Key(), child)
		}
		return result, nil
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		visit := contractValueVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if _, cyclic := active[visit]; cyclic {
			return reflect.Value{}, fmt.Errorf("value contains a slice cycle")
		}
		active[visit] = struct{}{}
		defer delete(active, visit)
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			child, err := cloneDeterministicValue(value.Index(index), active, preservePrivateFields)
			if err != nil {
				return reflect.Value{}, err
			}
			result.Index(index).Set(child)
		}
		return result, nil
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			child, err := cloneDeterministicValue(value.Index(index), active, preservePrivateFields)
			if err != nil {
				return reflect.Value{}, err
			}
			result.Index(index).Set(child)
		}
		return result, nil
	case reflect.Struct:
		if value.Type() == reflect.TypeFor[time.Time]() {
			// time.Time is immutable through its public API. A value copy safely
			// preserves its location and monotonic payload, whose fields cannot be
			// reconstructed with reflection outside package time.
			return value, nil
		}
		result := reflect.New(value.Type()).Elem()
		if preservePrivateFields {
			result.Set(value)
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if !field.IsExported() {
				if preservePrivateFields {
					continue
				}
				return reflect.Value{}, fmt.Errorf("struct type %s contains unexported field %s", value.Type(), field.Name)
			}
			child, err := cloneDeterministicValue(value.Field(index), active, preservePrivateFields)
			if err != nil {
				return reflect.Value{}, err
			}
			result.Field(index).Set(child)
		}
		return result, nil
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value, nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return reflect.Value{}, fmt.Errorf("floating-point value must be finite")
		}
		return value, nil
	default:
		return reflect.Value{}, fmt.Errorf("value type %s is not deterministic contract data", value.Type())
	}
}

func normalizeContractValue(value any, target reflect.Type) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("value cannot be nil")
	}
	source := reflect.ValueOf(value)
	if target.Kind() == reflect.Pointer {
		if source.Type().AssignableTo(target) {
			if source.IsNil() {
				return nil, fmt.Errorf("value cannot be nil")
			}
			value = source.Elem().Interface()
		}
		normalized, err := normalizeContractValue(value, target.Elem())
		if err != nil {
			return nil, err
		}
		pointer := reflect.New(target.Elem())
		pointer.Elem().Set(reflect.ValueOf(normalized))
		if pointer.Type() == target {
			return pointer.Interface(), nil
		}
		if pointer.Type().ConvertibleTo(target) {
			return pointer.Convert(target).Interface(), nil
		}
		return nil, fmt.Errorf("value has type %s, want %s", source.Type(), target)
	}
	if target.Kind() == reflect.Float32 || target.Kind() == reflect.Float64 {
		if source.Kind() == reflect.Float32 || source.Kind() == reflect.Float64 {
			number := source.Float()
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("value must be finite")
			}
		}
	}
	if source.Type().AssignableTo(target) {
		return value, nil
	}
	converted := reflect.New(target).Elem()
	switch target.Kind() {
	case reflect.String:
		if source.Kind() != reflect.String {
			return nil, fmt.Errorf("value has type %s, want %s", source.Type(), target)
		}
		converted.SetString(source.String())
	case reflect.Bool:
		if source.Kind() != reflect.Bool {
			return nil, fmt.Errorf("value has type %s, want %s", source.Type(), target)
		}
		converted.SetBool(source.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if source.Kind() < reflect.Int || source.Kind() > reflect.Int64 || converted.OverflowInt(source.Int()) {
			return nil, fmt.Errorf("value %v cannot be represented by %s", value, target)
		}
		converted.SetInt(source.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if source.Kind() < reflect.Uint || source.Kind() > reflect.Uintptr || converted.OverflowUint(source.Uint()) {
			return nil, fmt.Errorf("value %v cannot be represented by %s", value, target)
		}
		converted.SetUint(source.Uint())
	case reflect.Float32, reflect.Float64:
		if source.Kind() != reflect.Float32 && source.Kind() != reflect.Float64 {
			return nil, fmt.Errorf("value has type %s, want %s", source.Type(), target)
		}
		number := source.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("value must be finite")
		}
		if converted.OverflowFloat(number) {
			return nil, fmt.Errorf("value %v cannot be represented by %s", value, target)
		}
		converted.SetFloat(number)
	default:
		return nil, fmt.Errorf("value has type %s, want %s", source.Type(), target)
	}
	return converted.Interface(), nil
}

func numericKind(kind reflect.Kind) bool {
	return internalvaluekind.IsNumeric(kind)
}

func contractLiteralKind(kind reflect.Kind) bool {
	return internalvaluekind.IsScalar(kind)
}

func nilCapableType(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}

func compareContractValues(left, right any) int {
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	switch leftValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if leftValue.Int() < rightValue.Int() {
			return -1
		}
		if leftValue.Int() > rightValue.Int() {
			return 1
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if leftValue.Uint() < rightValue.Uint() {
			return -1
		}
		if leftValue.Uint() > rightValue.Uint() {
			return 1
		}
	case reflect.Float32, reflect.Float64:
		if leftValue.Float() < rightValue.Float() {
			return -1
		}
		if leftValue.Float() > rightValue.Float() {
			return 1
		}
	}
	return 0
}

// ValidateOperationInputs applies defaults and validates already-typed values.
// Transports perform their own wire decoding before calling this function.
func ValidateOperationInputs(contract OperationContract, supplied map[string]any) (OperationInputs, error) {
	normalized, err := normalizeOperationContract(contract)
	if err != nil {
		return OperationInputs{}, fmt.Errorf("koba: invalid operation contract: %w", err)
	}
	contract = normalized
	result := OperationInputs{values: make(map[string]operationInputValue, len(contract.Inputs))}
	for _, identifier := range sortedMapKeys(supplied) {
		if _, exists := contract.Inputs[identifier]; !exists {
			return OperationInputs{}, &InputValidationError{Input: identifier, Cause: fmt.Errorf("is not declared by the operation contract")}
		}
	}
	for _, identifier := range sortedMapKeys(contract.Inputs) {
		input := contract.Inputs[identifier]
		value, provided := supplied[identifier]
		if !provided {
			if input.HasDefault {
				result.values[identifier] = operationInputValue{value: appliedContractDefault(input)}
				continue
			}
			if input.Required {
				return OperationInputs{}, &InputValidationError{Input: identifier, Cause: fmt.Errorf("is required")}
			}
			continue
		}
		if isNilContractValue(value) {
			if !input.Nullable {
				return OperationInputs{}, &InputValidationError{Input: identifier, Cause: fmt.Errorf("must not be null")}
			}
			result.values[identifier] = operationInputValue{
				value:    reflect.Zero(input.GoType).Interface(),
				provided: true,
			}
			continue
		}
		normalized, err := normalizeContractValue(value, input.GoType)
		if err != nil {
			return OperationInputs{}, &InputValidationError{Input: identifier, Cause: err}
		}
		if err := validateInputValue(input, normalized); err != nil {
			return OperationInputs{}, &InputValidationError{Input: identifier, Cause: err}
		}
		result.values[identifier] = operationInputValue{value: normalized, provided: true}
	}
	return result, nil
}

func isNilContractValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return nilCapableType(reflected.Type()) && reflected.IsNil()
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateInputValue(input InputContract, value any) error {
	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return nil
	}
	// A typed nil slice is the decoded representation of JSON null, whereas a
	// non-nil zero-length slice represents an explicit empty array. Presence and
	// nullability are checked by the caller; cardinality only constrains arrays.
	if reflected.Kind() == reflect.Slice && reflected.IsNil() {
		return nil
	}
	value = reflected.Interface()
	if reflected.Kind() == reflect.Float32 || reflected.Kind() == reflect.Float64 {
		number := reflected.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("must be finite")
		}
	}
	if input.Minimum != nil && compareContractValues(value, input.Minimum) < 0 {
		return fmt.Errorf("must be greater than or equal to %v", input.Minimum)
	}
	if input.Maximum != nil && compareContractValues(value, input.Maximum) > 0 {
		return fmt.Errorf("must be less than or equal to %v", input.Maximum)
	}
	if reflected.Kind() == reflect.String {
		length := uint64(utf8.RuneCountInString(reflected.String()))
		if input.MinLength != nil && length < *input.MinLength {
			return fmt.Errorf("length must be at least %d", *input.MinLength)
		}
		if input.MaxLength != nil && length > *input.MaxLength {
			return fmt.Errorf("length must be at most %d", *input.MaxLength)
		}
		if input.Pattern != "" {
			matches, _ := regexp.MatchString(input.Pattern, reflected.String())
			if !matches {
				return fmt.Errorf("must match %q", input.Pattern)
			}
		}
	}
	if reflected.Kind() == reflect.Slice {
		items := uint64(reflected.Len())
		if input.MinItems != nil && items < *input.MinItems {
			return fmt.Errorf("must contain at least %d items", *input.MinItems)
		}
		if input.MaxItems != nil && items > *input.MaxItems {
			return fmt.Errorf("must contain at most %d items", *input.MaxItems)
		}
		if input.Items != nil {
			for index := 0; index < reflected.Len(); index++ {
				item := reflected.Index(index).Interface()
				if isNilContractValue(item) {
					if !input.Items.Input.Nullable {
						return fmt.Errorf("item %d: must not be null", index+1)
					}
					continue
				}
				if err := validateInputValue(input.Items.Input, item); err != nil {
					return fmt.Errorf("item %d: %w", index+1, err)
				}
				if err := validateCollectionElementValue(input.Items.Bindings, reflected.Index(index)); err != nil {
					return fmt.Errorf("item %d: %w", index+1, err)
				}
			}
		}
		if input.UniqueItems {
			for index := 0; index < reflected.Len(); index++ {
				for previous := 0; previous < index; previous++ {
					if reflect.DeepEqual(reflected.Index(previous).Interface(), reflected.Index(index).Interface()) {
						return fmt.Errorf("must contain unique items")
					}
				}
			}
		}
	}
	if len(input.Enum) > 0 {
		for _, allowed := range input.Enum {
			if reflect.DeepEqual(value, allowed) {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", input.Enum)
	}
	return nil
}

func validateCollectionElementValue(bindings []ResponseBinding, element reflect.Value) error {
	for _, binding := range bindings {
		value, present, err := collectionElementFieldValue(element, binding.FieldIndex)
		if err != nil {
			return fmt.Errorf("field %q: %w", displayResponseFieldPath(binding.FieldPath), err)
		}
		if !present {
			if binding.Input.Required {
				return fmt.Errorf("field %q: is required", displayResponseFieldPath(binding.FieldPath))
			}
			continue
		}
		fieldValue := value.Interface()
		if isNilContractValue(fieldValue) {
			if !binding.Input.Nullable {
				return fmt.Errorf("field %q: must not be null", displayResponseFieldPath(binding.FieldPath))
			}
			continue
		}
		if err := validateInputValue(binding.Input, fieldValue); err != nil {
			return fmt.Errorf("field %q: %w", displayResponseFieldPath(binding.FieldPath), err)
		}
	}
	return nil
}

func collectionElementFieldValue(element reflect.Value, index []int) (reflect.Value, bool, error) {
	current := element
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Interface || current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return reflect.Value{}, false, nil
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return reflect.Value{}, false, fmt.Errorf("field index %v is invalid", index[:position+1])
		}
		current = current.Field(fieldIndex)
	}
	if !current.IsValid() || !current.CanInterface() {
		return reflect.Value{}, false, fmt.Errorf("field is not accessible")
	}
	return current, true, nil
}
