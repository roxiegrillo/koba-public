package koba

import (
	"fmt"
	"reflect"
	"strings"
)

// ObjectRuleKind identifies a structural relationship between fields in one
// JSON object. Object rules describe field presence and null state only;
// domain-dependent value rules remain in the API function or domain layer.
type ObjectRuleKind string

const (
	// ObjectRuleAtLeastOne requires at least one referenced field to be present.
	ObjectRuleAtLeastOne ObjectRuleKind = "at_least_one"
	// ObjectRuleAllOrNone requires either every referenced field or none of them
	// to be present.
	ObjectRuleAllOrNone ObjectRuleKind = "all_or_none"
	// ObjectRuleSameNullState rejects a mix of null and non-null values among
	// the referenced fields which are present.
	ObjectRuleSameNullState ObjectRuleKind = "same_null_state"
)

// ObjectRule is an immutable authoring value attached to BodyObject with
// WithRules. Field paths use exported Go field names relative to that body
// object, with dots separating nested fields (for example, "Location.Lat").
//
// Presence is independent from value: an explicitly supplied JSON null counts
// as present. The field's own binding or decoder determines whether null is an
// allowed value.
type ObjectRule struct {
	kind   ObjectRuleKind
	fields []string
}

// AtLeastOne requires at least one of fields to be present in the JSON object.
func AtLeastOne(fields ...string) ObjectRule {
	return ObjectRule{kind: ObjectRuleAtLeastOne, fields: append([]string(nil), fields...)}
}

// AllOrNone requires either all referenced fields or none of them to be
// present in the JSON object.
func AllOrNone(fields ...string) ObjectRule {
	return ObjectRule{kind: ObjectRuleAllOrNone, fields: append([]string(nil), fields...)}
}

// SameNullState requires the referenced fields which are present to be either
// all explicitly null or all non-null. Compose it with AllOrNone when the
// fields must also be supplied together.
func SameNullState(fields ...string) ObjectRule {
	return ObjectRule{kind: ObjectRuleSameNullState, fields: append([]string(nil), fields...)}
}

// Kind returns the rule's structural relationship.
func (rule ObjectRule) Kind() ObjectRuleKind { return rule.kind }

// Fields returns a copy of the Go field paths referenced by the rule.
func (rule ObjectRule) Fields() []string { return append([]string(nil), rule.fields...) }

func normalizeObjectRules(rules []ObjectRule, objectType reflect.Type) ([]ObjectRule, error) {
	if rules == nil {
		return nil, nil
	}
	result := make([]ObjectRule, 0, len(rules))
	for index, rule := range rules {
		normalized, err := normalizeObjectRule(rule, objectType)
		if err != nil {
			return nil, fmt.Errorf("object rule %d: %w", index+1, err)
		}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeObjectRule(rule ObjectRule, objectType reflect.Type) (ObjectRule, error) {
	minimumFields := 1
	switch rule.kind {
	case ObjectRuleAtLeastOne:
	case ObjectRuleAllOrNone:
		minimumFields = 2
	case ObjectRuleSameNullState:
		minimumFields = 2
	default:
		return ObjectRule{}, fmt.Errorf("unsupported kind %q", rule.kind)
	}
	if len(rule.fields) < minimumFields {
		return ObjectRule{}, fmt.Errorf("%s requires at least %d field(s)", rule.kind, minimumFields)
	}

	normalized := ObjectRule{kind: rule.kind, fields: make([]string, 0, len(rule.fields))}
	seen := make(map[string]struct{}, len(rule.fields))
	for _, raw := range rule.fields {
		path, err := normalizedObjectRuleField(raw)
		if err != nil {
			return ObjectRule{}, err
		}
		if err := validateObjectRuleField(objectType, path); err != nil {
			return ObjectRule{}, err
		}
		if rule.kind == ObjectRuleSameNullState {
			fieldType, err := objectRuleFieldType(objectType, path)
			if err != nil {
				return ObjectRule{}, err
			}
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Interface {
				return ObjectRule{}, fmt.Errorf(
					"field %q must have a concrete Go type for %s",
					path,
					rule.kind,
				)
			}
		}
		if _, duplicate := seen[path]; duplicate {
			return ObjectRule{}, fmt.Errorf("field %q is repeated", path)
		}
		seen[path] = struct{}{}
		normalized.fields = append(normalized.fields, path)
	}
	return normalized, nil
}

func validateObjectRuleField(objectType reflect.Type, path string) error {
	_, err := objectRuleFieldType(objectType, path)
	return err
}

func objectRuleFieldType(objectType reflect.Type, path string) (reflect.Type, error) {
	current := objectType
	for _, name := range strings.Split(path, ".") {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return nil, fmt.Errorf("field %q traverses non-struct type %s", path, current)
		}
		field, exists := directStructField(current, name)
		if !exists || !field.IsExported() {
			return nil, fmt.Errorf("field %q does not exist as an exported field in %s", path, current)
		}
		if jsonName := strings.Split(field.Tag.Get("json"), ",")[0]; jsonName == "-" {
			return nil, fmt.Errorf("field %q is excluded from JSON", path)
		}
		current = field.Type
	}
	return current, nil
}

func objectRuleJSONPath(objectType reflect.Type, path string) (string, error) {
	current := objectType
	segments := strings.Split(path, ".")
	for index, name := range segments {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return "", fmt.Errorf("field %q traverses non-struct type %s", path, current)
		}
		field, exists := directStructField(current, name)
		if !exists || !field.IsExported() {
			return "", fmt.Errorf("field %q does not exist as an exported field in %s", path, current)
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			return "", fmt.Errorf("field %q is excluded from JSON", path)
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		segments[index] = jsonName
		current = field.Type
	}
	return strings.Join(segments, "."), nil
}

func normalizedObjectRuleField(path string) (string, error) {
	segments := strings.Split(path, ".")
	for index := range segments {
		segments[index] = strings.TrimSpace(segments[index])
		if segments[index] == "" {
			return "", fmt.Errorf("field path %q contains an empty segment", path)
		}
	}
	return strings.Join(segments, "."), nil
}
