package openapi

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

func newSchemaGenerator(schemas openapi3.Schemas, customizer SchemaCustomizer) *openapi3gen.Generator {
	return openapi3gen.NewGenerator(
		openapi3gen.UseAllExportedFields(),
		openapi3gen.CreateComponentSchemas(openapi3gen.ExportComponentSchemasOptions{
			ExportComponentSchemas: true,
			ExportTopLevelSchema:   true,
			ExportGenerics:         true,
		}),
		openapi3gen.CreateTypeNameGenerator(componentName),
		openapi3gen.SchemaCustomizer(func(name string, valueType reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
			if valueType == reflect.TypeFor[fbtypes.Time]() {
				*schema = *openapi3.NewDateTimeSchema()
			}
			if valueType == reflect.TypeFor[fbtypes.ProbeStatus]() {
				schema.Description = "Status returned by the framework-owned operational probes."
				if status := schema.Properties["status"]; status != nil && status.Value != nil {
					status.Value.Description = "One of ok, ready, or not_ready according to the requested probe."
					status.Value.Enum = []any{"ok", "ready", "not_ready"}
				}
			}
			if customizer != nil {
				if err := customizer(SchemaContext{Name: name, GoType: valueType, StructTag: tag, Schema: schema}); err != nil {
					return err
				}
			}
			// openapi3gen only exports component values with object properties.
			// Register all named struct schemas here as well so empty structs and
			// custom scalar marshalers retain resolvable component references.
			if schemas != nil && valueType.Kind() == reflect.Struct && valueType != reflect.TypeFor[time.Time]() && valueType.Name() != "" {
				if schema.Type == nil {
					schema.Type = &openapi3.Types{"object"}
				}
				schemas[componentName(valueType)] = &openapi3.SchemaRef{Value: schema}
			}
			return nil
		}),
	)
}

func schemaForType(generator *openapi3gen.Generator, schemas openapi3.Schemas, valueType reflect.Type) (*openapi3.SchemaRef, error) {
	if valueType == nil {
		return nil, fmt.Errorf("Go type is nil")
	}
	underlying := valueType
	for underlying.Kind() == reflect.Pointer {
		underlying = underlying.Elem()
	}
	if underlying.Kind() == reflect.Interface {
		return &openapi3.SchemaRef{Value: openapi3.NewSchema()}, nil
	}
	value := reflect.Zero(valueType).Interface()
	// openapi3gen expects a pointer when exporting a top-level struct into the
	// supplied components map. Passing the struct value can return a component
	// $ref without registering the referenced schema.
	if valueType.Kind() == reflect.Struct {
		value = reflect.New(valueType).Interface()
	}
	ref, err := generator.NewSchemaRefForValue(value, schemas)
	if err != nil {
		return nil, err
	}
	applyBindingConstraints(valueType, ref, schemas, make(map[reflect.Type]struct{}))
	return ref, nil
}

func responseSchemaForType(generator *openapi3gen.Generator, schemas openapi3.Schemas, valueType reflect.Type) (*openapi3.SchemaRef, error) {
	ref, err := schemaForType(generator, schemas, valueType)
	if err != nil {
		return nil, err
	}
	if valueType.Kind() != reflect.Pointer {
		return ref, nil
	}
	return &openapi3.SchemaRef{Value: &openapi3.Schema{
		Nullable: true,
		AllOf:    openapi3.SchemaRefs{ref},
	}}, nil
}

// responsePresenceSchema derives JSON response presence from the declared Go
// wire type without mutating shared component schemas. A field without
// omitempty is serialized as a property and is therefore required; pointer
// nullability remains represented by the reflected child schema.
func responsePresenceSchema(
	ref *openapi3.SchemaRef,
	valueType reflect.Type,
	schemas openapi3.Schemas,
	seen map[reflect.Type]struct{},
) (*openapi3.SchemaRef, error) {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if _, exists := seen[valueType]; exists {
		return ref, nil
	}
	clone, err := mutableSchemaRef(ref, schemas)
	if err != nil {
		return nil, err
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if valueType == reflect.TypeFor[time.Time]() || valueType == reflect.TypeFor[fbtypes.Time]() {
			return clone, nil
		}
		seen[valueType] = struct{}{}
		defer delete(seen, valueType)
		for _, field := range reflect.VisibleFields(valueType) {
			if field.PkgPath != "" {
				continue
			}
			jsonName, included := openAPIJSONFieldName(field)
			if !included || (field.Anonymous && strings.Split(field.Tag.Get("json"), ",")[0] == "") {
				continue
			}
			property := clone.Value.Properties[jsonName]
			if property == nil {
				continue
			}
			if !jsonTagHasOption(field.Tag.Get("json"), "omitempty") {
				clone.Value.Required = appendUnique(clone.Value.Required, jsonName)
			}
			child, childErr := responsePresenceSchema(property, field.Type, schemas, seen)
			if childErr != nil {
				return nil, childErr
			}
			clone.Value.Properties[jsonName] = child
		}
		sort.Strings(clone.Value.Required)
	case reflect.Array, reflect.Slice:
		if clone.Value.Items != nil {
			items, childErr := responsePresenceSchema(clone.Value.Items, valueType.Elem(), schemas, seen)
			if childErr != nil {
				return nil, childErr
			}
			clone.Value.Items = items
		}
	case reflect.Map:
		if clone.Value.AdditionalProperties.Schema != nil {
			additional, childErr := responsePresenceSchema(clone.Value.AdditionalProperties.Schema, valueType.Elem(), schemas, seen)
			if childErr != nil {
				return nil, childErr
			}
			clone.Value.AdditionalProperties.Schema = additional
		}
	}
	return clone, nil
}

func jsonTagHasOption(tag string, option string) bool {
	parts := strings.Split(tag, ",")
	for _, part := range parts[1:] {
		if part == option {
			return true
		}
	}
	return false
}

func applyBindingConstraints(valueType reflect.Type, ref *openapi3.SchemaRef, schemas openapi3.Schemas, seen map[reflect.Type]struct{}) {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if _, visited := seen[valueType]; visited {
		return
	}
	schema := resolvedSchema(ref, schemas)
	if schema == nil {
		return
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if valueType == reflect.TypeFor[time.Time]() || valueType == reflect.TypeFor[fbtypes.Time]() {
			return
		}
		seen[valueType] = struct{}{}
		for _, field := range reflect.VisibleFields(valueType) {
			if field.PkgPath != "" {
				continue
			}
			jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonName == "-" {
				continue
			}
			if field.Anonymous && jsonName == "" {
				// Promoted fields are also returned by VisibleFields and are
				// processed against this parent schema individually.
				continue
			}
			if jsonName == "" {
				jsonName = field.Name
			}
			property := schema.Properties[jsonName]
			if property == nil {
				continue
			}
			if bindingRequiresValue(field.Tag.Get("binding")) {
				schema.Required = appendUnique(schema.Required, jsonName)
			}
			applyBindingConstraints(field.Type, property, schemas, seen)
		}
		sort.Strings(schema.Required)
	case reflect.Array, reflect.Slice:
		if schema.Items != nil {
			applyBindingConstraints(valueType.Elem(), schema.Items, schemas, seen)
		}
	case reflect.Map:
		if schema.AdditionalProperties.Schema != nil {
			applyBindingConstraints(valueType.Elem(), schema.AdditionalProperties.Schema, schemas, seen)
		}
	}
}

func resolvedSchema(ref *openapi3.SchemaRef, schemas openapi3.Schemas) *openapi3.Schema {
	if ref == nil {
		return nil
	}
	if ref.Value != nil {
		return ref.Value
	}
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref.Ref, prefix) {
		if component := schemas[strings.TrimPrefix(ref.Ref, prefix)]; component != nil {
			return component.Value
		}
	}
	return nil
}

func bindingRequiresValue(binding string) bool {
	for _, rule := range strings.Split(binding, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "dive" {
			return false
		}
		if rule == "required" {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

var componentCharacter = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func componentName(valueType reflect.Type) string {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	name := valueType.Name()
	if name == "" {
		name = valueType.String()
	}
	if packagePath := valueType.PkgPath(); packagePath != "" {
		// Use the complete package path. Package basenames are not unique, and
		// silently reusing one component for two distinct Go types corrupts the
		// generated contract.
		name = packagePath + "." + name
	}
	return strings.Trim(componentCharacter.ReplaceAllString(name, "_"), "_")
}
