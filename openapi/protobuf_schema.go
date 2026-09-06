package openapi

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	internalprotobufdescriptor "github.com/roxiegrillo/koba/v2/internal/protobufdescriptor"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// protobufSocketSchemaReference renders the JSON shape accepted and emitted by
// protojson. It intentionally uses descriptors instead of generated Go struct
// fields: protobuf int64 values are JSON strings, timestamps are RFC 3339
// strings, and field names follow the proto spelling used by generated clients.
func protobufSocketSchemaReference(schemas openapi3.Schemas, customizer SchemaCustomizer, valueType reflect.Type) (string, error) {
	descriptor, err := internalprotobufdescriptor.MessageDescriptor(valueType)
	if err != nil {
		return "", err
	}
	ref, err := ensureProtobufMessageSchema(schemas, descriptor)
	if err != nil {
		return "", err
	}
	if customizer != nil {
		name := protobufComponentName(descriptor.FullName())
		component := schemas[name]
		if component == nil || component.Value == nil {
			return "", fmt.Errorf("protobuf schema component %q was not registered", name)
		}
		if err := customizer(SchemaContext{Name: name, GoType: valueType, Schema: component.Value}); err != nil {
			return "", err
		}
	}
	return ref.Ref, nil
}

func ensureProtobufMessageSchema(
	schemas openapi3.Schemas,
	descriptor protoreflect.MessageDescriptor,
) (*openapi3.SchemaRef, error) {
	name := protobufComponentName(descriptor.FullName())
	reference := "#/components/schemas/" + name
	if existing := schemas[name]; existing != nil {
		return &openapi3.SchemaRef{Ref: reference}, nil
	}

	// Register a placeholder before descending so recursive messages resolve.
	component := &openapi3.SchemaRef{Value: openapi3.NewObjectSchema()}
	schemas[name] = component
	schema, err := protobufMessageSchema(schemas, descriptor)
	if err != nil {
		delete(schemas, name)
		return nil, err
	}
	component.Value = schema
	return &openapi3.SchemaRef{Ref: reference}, nil
}

func protobufMessageSchema(
	schemas openapi3.Schemas,
	descriptor protoreflect.MessageDescriptor,
) (*openapi3.Schema, error) {
	if wellKnown := protobufWellKnownSchema(descriptor.FullName()); wellKnown != nil {
		return wellKnown, nil
	}
	schema := openapi3.NewObjectSchema()
	schema.Extensions = map[string]any{"x-protobuf-message": string(descriptor.FullName())}
	fields := descriptor.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		fieldSchema, err := protobufFieldSchema(schemas, field)
		if err != nil {
			return nil, fmt.Errorf("protobuf field %s: %w", field.FullName(), err)
		}
		// api-cli uses proto names for stable snake_case input and output. Standard
		// protojson decoders also accept the lowerCamel JSON name.
		name := string(field.Name())
		schema.Properties[name] = fieldSchema
		if field.Cardinality() == protoreflect.Required {
			schema.Required = append(schema.Required, name)
		}
	}
	return schema, nil
}

func protobufFieldSchema(schemas openapi3.Schemas, field protoreflect.FieldDescriptor) (*openapi3.SchemaRef, error) {
	if field.IsMap() {
		value, err := protobufSingularFieldSchema(schemas, field.MapValue())
		if err != nil {
			return nil, err
		}
		schema := openapi3.NewObjectSchema()
		schema.AdditionalProperties = openapi3.AdditionalProperties{Schema: value}
		return &openapi3.SchemaRef{Value: schema}, nil
	}
	value, err := protobufSingularFieldSchema(schemas, field)
	if err != nil {
		return nil, err
	}
	if field.IsList() {
		schema := openapi3.NewArraySchema()
		schema.Items = value
		return &openapi3.SchemaRef{Value: schema}, nil
	}
	return value, nil
}

func protobufSingularFieldSchema(schemas openapi3.Schemas, field protoreflect.FieldDescriptor) (*openapi3.SchemaRef, error) {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()}, nil
	case protoreflect.StringKind:
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}, nil
	case protoreflect.BytesKind:
		return &openapi3.SchemaRef{Value: openapi3.NewBytesSchema()}, nil
	case protoreflect.EnumKind:
		schema := openapi3.NewStringSchema()
		values := field.Enum().Values()
		for index := 0; index < values.Len(); index++ {
			schema.Enum = append(schema.Enum, string(values.Get(index).Name()))
		}
		return &openapi3.SchemaRef{Value: schema}, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return &openapi3.SchemaRef{Value: openapi3.NewInt32Schema()}, nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return &openapi3.SchemaRef{Value: openapi3.NewIntegerSchema().WithFormat("int64").WithMin(0).WithMax(4294967295)}, nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithPattern(`^-?[0-9]+$`)}, nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithPattern(`^[0-9]+$`)}, nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		finite := openapi3.NewFloat64Schema()
		special := openapi3.NewStringSchema().WithEnum("NaN", "Infinity", "-Infinity")
		return &openapi3.SchemaRef{Value: openapi3.NewOneOfSchema(finite, special)}, nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if wellKnown := protobufWellKnownSchema(field.Message().FullName()); wellKnown != nil {
			return &openapi3.SchemaRef{Value: wellKnown}, nil
		}
		return ensureProtobufMessageSchema(schemas, field.Message())
	default:
		return nil, fmt.Errorf("unsupported protobuf kind %s", field.Kind())
	}
}

func protobufWellKnownSchema(name protoreflect.FullName) *openapi3.Schema {
	switch name {
	case "google.protobuf.Timestamp":
		return openapi3.NewDateTimeSchema()
	case "google.protobuf.Duration", "google.protobuf.FieldMask":
		return openapi3.NewStringSchema()
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue":
		return openapi3.NewOneOfSchema(
			openapi3.NewFloat64Schema(),
			openapi3.NewStringSchema().WithEnum("NaN", "Infinity", "-Infinity"),
		)
	case "google.protobuf.Int64Value":
		return openapi3.NewStringSchema().WithPattern(`^-?[0-9]+$`)
	case "google.protobuf.UInt64Value":
		return openapi3.NewStringSchema().WithPattern(`^[0-9]+$`)
	case "google.protobuf.Int32Value":
		return openapi3.NewIntegerSchema().WithFormat("int32")
	case "google.protobuf.UInt32Value":
		return openapi3.NewIntegerSchema().WithFormat("int64").WithMin(0).WithMax(4294967295)
	case "google.protobuf.BoolValue":
		return openapi3.NewBoolSchema()
	case "google.protobuf.StringValue":
		return openapi3.NewStringSchema()
	case "google.protobuf.BytesValue":
		return openapi3.NewBytesSchema()
	case "google.protobuf.Struct":
		schema := openapi3.NewObjectSchema()
		allow := true
		schema.AdditionalProperties = openapi3.AdditionalProperties{Has: &allow}
		return schema
	case "google.protobuf.Value":
		return openapi3.NewSchema()
	case "google.protobuf.NullValue":
		return &openapi3.Schema{Nullable: true, Enum: []any{nil}}
	case "google.protobuf.ListValue":
		return openapi3.NewArraySchema().WithItems(openapi3.NewSchema())
	case "google.protobuf.Any":
		schema := openapi3.NewObjectSchema().WithProperty("@type", openapi3.NewStringSchema())
		schema.Required = []string{"@type"}
		allow := true
		schema.AdditionalProperties = openapi3.AdditionalProperties{Has: &allow}
		return schema
	case "google.protobuf.Empty":
		return openapi3.NewObjectSchema()
	default:
		return nil
	}
}

func protobufComponentName(fullName protoreflect.FullName) string {
	return strings.Trim(componentCharacter.ReplaceAllString("protobuf."+string(fullName), "_"), "_")
}
