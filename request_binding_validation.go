package koba

import (
	"fmt"
	"reflect"
	"strings"
)

func normalizeResolvedRequestBindings(
	bindings []RequestBinding,
	inputType reflect.Type,
) ([]RequestBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	if inputType == nil {
		return nil, fmt.Errorf("request bindings require an operation input type")
	}
	result := make([]RequestBinding, 0, len(bindings))
	wire := make(map[string]string)
	bodyIndex := -1
	for index, binding := range bindings {
		path := displayRequestFieldPath(binding.FieldPath)
		targetType, err := requestTypeAtIndex(inputType, binding.FieldIndex)
		if err != nil {
			return nil, fmt.Errorf("request binding %q: %w", path, err)
		}
		resolvedPath, err := requestPathAtIndex(inputType, binding.FieldIndex)
		if err != nil {
			return nil, fmt.Errorf("request binding %q: %w", path, err)
		}
		if !requestPathsEqual(resolvedPath, binding.FieldPath) {
			return nil, fmt.Errorf(
				"request binding %q has field index %v for %q",
				path,
				binding.FieldIndex,
				displayRequestFieldPath(resolvedPath),
			)
		}
		if targetType != binding.Input.GoType {
			return nil, fmt.Errorf(
				"request binding %q has type %s, but field has type %s",
				path,
				binding.Input.GoType,
				targetType,
			)
		}
		normalized, err := normalizeInputContract(binding.Input)
		if err != nil {
			return nil, fmt.Errorf("request binding %q: %w", path, err)
		}
		binding.Input = normalized
		binding.FieldPath = append([]string(nil), binding.FieldPath...)
		binding.FieldIndex = append([]int(nil), binding.FieldIndex...)
		switch binding.Source {
		case BindingSourceQuery, BindingSourcePath, BindingSourceHeader, BindingSourceCookie:
			if strings.TrimSpace(binding.Name) == "" {
				return nil, fmt.Errorf("request binding %q has an empty %s name", path, binding.Source)
			}
			if !requestScalarType(targetType) {
				return nil, fmt.Errorf("request binding %q uses non-scalar %s as %s", path, targetType, binding.Source)
			}
			key := requestWireBindingKey(binding.Source, binding.Name)
			if previous, duplicate := wire[key]; duplicate {
				return nil, fmt.Errorf("request bindings %q and %q both bind %s %q", previous, path, binding.Source, binding.Name)
			}
			wire[key] = path
			if binding.Source == BindingSourcePath && !binding.Input.Required {
				return nil, fmt.Errorf("path request binding %q must be required", path)
			}
		case BindingSourceBody:
			if bodyIndex >= 0 {
				return nil, fmt.Errorf(
					"request bindings %q and %q both bind the request body",
					displayRequestFieldPath(result[bodyIndex].FieldPath),
					path,
				)
			}
			bodyIndex = index
		case BindingSourceValue:
		default:
			return nil, fmt.Errorf("request binding %q has unsupported source %q", path, binding.Source)
		}
		result = append(result, binding)
	}
	if bodyIndex < 0 {
		for _, binding := range result {
			if binding.Source == BindingSourceValue {
				return nil, fmt.Errorf("request binding %q is a body value without a body object", displayRequestFieldPath(binding.FieldPath))
			}
		}
		return result, nil
	}
	bodyPath := result[bodyIndex].FieldPath
	for _, binding := range result {
		if binding.Source != BindingSourceValue {
			continue
		}
		if !requestPathHasPrefix(binding.FieldPath, bodyPath) || len(binding.FieldPath) == len(bodyPath) {
			return nil, fmt.Errorf(
				"request binding %q is outside body object %q",
				displayRequestFieldPath(binding.FieldPath),
				displayRequestFieldPath(bodyPath),
			)
		}
	}
	return result, nil
}

func requestPathAtIndex(inputType reflect.Type, index []int) ([]string, error) {
	current := inputType
	path := make([]string, 0, len(index))
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return nil, fmt.Errorf("field index %v is invalid for %s", index[:position+1], current)
		}
		field := current.Field(fieldIndex)
		path = append(path, field.Name)
		current = field.Type
	}
	return path, nil
}

func requestTypeAtIndex(inputType reflect.Type, index []int) (reflect.Type, error) {
	current := inputType
	if len(index) == 0 {
		return current, nil
	}
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return nil, fmt.Errorf("field index %v is invalid for %s", index[:position+1], current)
		}
		current = current.Field(fieldIndex).Type
	}
	return current, nil
}

func requestPathHasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for index := range prefix {
		if path[index] != prefix[index] {
			return false
		}
	}
	return true
}

func requestPathsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
