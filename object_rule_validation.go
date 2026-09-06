package koba

import (
	"fmt"
	"reflect"
	"strings"
)

func validateBodyObjectRules(
	body RequestBinding,
	document any,
) error {
	for _, rule := range body.ObjectRules {
		present := 0
		nullPresent := 0
		nonNullPresent := 0
		displayFields := make([]string, 0, len(rule.fields))
		for _, field := range rule.fields {
			exists, null, err := bodyObjectRuleFieldState(document, body.Input.GoType, field)
			if err != nil {
				return requestBindingError(body, err)
			}
			display, err := objectRuleJSONPath(body.Input.GoType, field)
			if err != nil {
				return requestBindingError(body, err)
			}
			displayFields = append(displayFields, display)
			if exists {
				present++
				if null {
					nullPresent++
				} else {
					nonNullPresent++
				}
			}
		}

		switch rule.kind {
		case ObjectRuleAtLeastOne:
			if present == 0 {
				return requestBindingError(body, fmt.Errorf(
					"at least one of %s must be provided",
					strings.Join(displayFields, ", "),
				))
			}
		case ObjectRuleAllOrNone:
			if present != 0 && present != len(rule.fields) {
				return requestBindingError(body, fmt.Errorf(
					"%s must be provided together",
					strings.Join(displayFields, ", "),
				))
			}
		case ObjectRuleSameNullState:
			if nullPresent != 0 && nonNullPresent != 0 {
				return requestBindingError(body, fmt.Errorf(
					"%s must be either all null or all non-null",
					strings.Join(displayFields, ", "),
				))
			}
		default:
			return requestBindingError(body, fmt.Errorf("unsupported object rule kind %q", rule.kind))
		}
	}
	return nil
}

func bodyObjectRuleFieldState(
	document any,
	bodyType reflect.Type,
	fieldPath string,
) (present bool, null bool, err error) {
	current := document
	currentType := bodyType
	segments := strings.Split(fieldPath, ".")
	for index, fieldName := range segments {
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
		if currentType.Kind() != reflect.Struct {
			return false, false, fmt.Errorf("field %q traverses non-struct type %s", fieldPath, currentType)
		}
		field, exists := directStructField(currentType, fieldName)
		if !exists {
			return false, false, fmt.Errorf("field %q does not exist in %s", fieldPath, currentType)
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			return false, false, fmt.Errorf("field %q is excluded from JSON", fieldPath)
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		object, ok := current.(map[string]any)
		if !ok {
			return false, false, nil
		}
		value, exists, err := jsonObjectValue(object, jsonName)
		if err != nil {
			return false, false, err
		}
		if !exists {
			return false, false, nil
		}
		if index == len(segments)-1 {
			return true, value == nil, nil
		}
		if value == nil {
			// A null ancestor does not make any of its descendants present.
			return false, false, nil
		}
		current = value
		currentType = field.Type
	}
	return false, false, nil
}
