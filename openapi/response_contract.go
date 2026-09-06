package openapi

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

func responseContractSchema(
	ref *openapi3.SchemaRef,
	responseType reflect.Type,
	bindings []fb.ResponseBinding,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	rootRef := ref
	wrapNullable := responseType.Kind() == reflect.Pointer && ref != nil && ref.Value != nil && ref.Value.Nullable && len(ref.Value.AllOf) == 1
	if wrapNullable {
		rootRef = ref.Value.AllOf[0]
	}
	// Response presence belongs to the opt-in response companion. Preserve the
	// historical component reference when an operation has no ResponseBindings;
	// callers only reach this function when bindings are present.
	root, err := responsePresenceSchema(rootRef, responseType, schemas, make(map[reflect.Type]struct{}))
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		if len(binding.FieldPath) == 0 {
			if responseBindingEntersElement(binding.ElementAt, 0) {
				list := resolvedSchema(root, schemas)
				if list == nil || list.Items == nil {
					return nil, fmt.Errorf("root response list has no item schema")
				}
				item, itemErr := mutableSchemaRef(list.Items, schemas)
				if itemErr != nil {
					return nil, fmt.Errorf("root response list item: %w", itemErr)
				}
				list.Items, itemErr = documentedInputContractSchema(
					item,
					binding.Input,
					binding.Documentation,
					schemas,
				)
				if itemErr != nil {
					return nil, fmt.Errorf("root response list item: %w", itemErr)
				}
			} else {
				root, err = documentedInputContractSchema(root, binding.Input, binding.Documentation, schemas)
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := applyResponseValueContract(root, responseType, binding, schemas); err != nil {
			return nil, err
		}
	}
	if !wrapNullable {
		return root, nil
	}
	return &openapi3.SchemaRef{Value: &openapi3.Schema{
		Nullable: true,
		AllOf:    openapi3.SchemaRefs{root},
	}}, nil
}

func applyResponseValueContract(
	root *openapi3.SchemaRef,
	responseType reflect.Type,
	binding fb.ResponseBinding,
	schemas openapi3.Schemas,
) error {
	currentRef := root
	currentType := responseType
	for currentType.Kind() == reflect.Pointer {
		currentType = currentType.Elem()
	}
	elements := make(map[int]struct{}, len(binding.ElementAt))
	for _, offset := range binding.ElementAt {
		elements[offset] = struct{}{}
	}
	for index, fieldName := range binding.FieldPath {
		if _, entersElement := elements[index]; entersElement {
			if currentType.Kind() != reflect.Slice && currentType.Kind() != reflect.Array {
				return fmt.Errorf("response field %s enters an element of non-list type %s", strings.Join(binding.FieldPath, "."), currentType)
			}
			current := resolvedSchema(currentRef, schemas)
			if current == nil || current.Items == nil {
				return fmt.Errorf("response field %s has no item schema", strings.Join(binding.FieldPath, "."))
			}
			item, err := mutableSchemaRef(current.Items, schemas)
			if err != nil {
				return fmt.Errorf("response field %s item: %w", strings.Join(binding.FieldPath, "."), err)
			}
			current.Items = item
			currentRef = item
			currentType = currentType.Elem()
			for currentType.Kind() == reflect.Pointer {
				currentType = currentType.Elem()
			}
		}
		if currentType.Kind() != reflect.Struct {
			return fmt.Errorf("response field %s traverses non-struct type %s", strings.Join(binding.FieldPath, "."), currentType)
		}
		field, exists := directOpenAPIField(currentType, fieldName)
		if !exists {
			return fmt.Errorf("response field %s references missing field %s", strings.Join(binding.FieldPath, "."), fieldName)
		}
		jsonName, included := openAPIJSONFieldName(field)
		if !included {
			return fmt.Errorf("response field %s references JSON-excluded field %s", strings.Join(binding.FieldPath, "."), fieldName)
		}
		current := resolvedSchema(currentRef, schemas)
		if current == nil {
			return fmt.Errorf("response field %s has an unresolved parent schema", strings.Join(binding.FieldPath, "."))
		}
		property := current.Properties[jsonName]
		if property == nil {
			return fmt.Errorf("response field %s has no schema property %q", strings.Join(binding.FieldPath, "."), jsonName)
		}
		targetsElement := responseBindingEntersElement(binding.ElementAt, index+1)
		if binding.Input.Required && !targetsElement {
			current.Required = appendUnique(current.Required, jsonName)
			sort.Strings(current.Required)
		}
		if index == len(binding.FieldPath)-1 {
			if targetsElement {
				list, err := mutableSchemaRef(property, schemas)
				if err != nil {
					return fmt.Errorf("response field %s list: %w", strings.Join(binding.FieldPath, "."), err)
				}
				if list.Value.Items == nil {
					return fmt.Errorf("response field %s has no item schema", strings.Join(binding.FieldPath, "."))
				}
				item, err := mutableSchemaRef(list.Value.Items, schemas)
				if err != nil {
					return fmt.Errorf("response field %s item: %w", strings.Join(binding.FieldPath, "."), err)
				}
				list.Value.Items, err = documentedInputContractSchema(
					item,
					binding.Input,
					binding.Documentation,
					schemas,
				)
				if err != nil {
					return fmt.Errorf("response field %s item: %w", strings.Join(binding.FieldPath, "."), err)
				}
				current.Properties[jsonName] = list
			} else {
				contracted, err := documentedInputContractSchema(
					property,
					binding.Input,
					binding.Documentation,
					schemas,
				)
				if err != nil {
					return fmt.Errorf("response field %s: %w", strings.Join(binding.FieldPath, "."), err)
				}
				current.Properties[jsonName] = contracted
			}
			return nil
		}
		child, err := mutableSchemaRef(property, schemas)
		if err != nil {
			return fmt.Errorf("response field %s: field %s: %w", strings.Join(binding.FieldPath, "."), fieldName, err)
		}
		current.Properties[jsonName] = child
		currentRef = child
		currentType = field.Type
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
	}
	return nil
}

func responseBindingEntersElement(offsets []int, want int) bool {
	for _, offset := range offsets {
		if offset == want {
			return true
		}
	}
	return false
}
