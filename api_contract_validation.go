package koba

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
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
	normalized := OperationContract{
		Inputs:  make(map[string]InputContract, len(operation.Inputs)),
		Request: operation.Request,
		Authorization: AuthorizationContract{
			RequiredScopes: append([]string(nil), operation.Authorization.RequiredScopes...),
		},
	}
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

	valueType := input.GoType
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	kind := valueType.Kind()
	if (input.HasDefault || input.Enum != nil) && !contractLiteralKind(kind) {
		return InputContract{}, fmt.Errorf("default and enum require a scalar Go type, got %s", input.GoType)
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
		if err := validateInputValue(result, result.Default); err != nil {
			return InputContract{}, fmt.Errorf("default: %w", err)
		}
	}
	return result, nil
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
		if pointer.Type().AssignableTo(target) {
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
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func contractLiteralKind(kind reflect.Kind) bool {
	return kind == reflect.String || kind == reflect.Bool || numericKind(kind)
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
				result.values[identifier] = operationInputValue{value: input.Default}
				continue
			}
			if input.Required {
				return OperationInputs{}, &InputValidationError{Input: identifier, Cause: fmt.Errorf("is required")}
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
	value = reflected.Interface()
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
