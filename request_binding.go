package koba

import (
	"fmt"
	"reflect"
	"strings"
)

// BindingSource identifies the transport value used to populate a request
// field. Body bindings decode the complete request body into their target
// field; consequently an operation may contain at most one body binding.
type BindingSource string

const (
	BindingSourceQuery  BindingSource = "query"
	BindingSourcePath   BindingSource = "path"
	BindingSourceHeader BindingSource = "header"
	BindingSourceCookie BindingSource = "cookie"
	BindingSourceBody   BindingSource = "body"
	BindingSourceValue  BindingSource = "value"
)

// Binding is a typed leaf in a request companion struct. Its type parameter
// must exactly match the corresponding field in the operation request,
// including named and pointer types. Bindings are constructed with Query,
// Path, Header, Cookie, or Value rather than struct literals so their Go type
// cannot drift from T.
type Binding[T any] struct {
	source   BindingSource
	name     string
	contract InputContract
}

type bindingMetadata struct {
	source   BindingSource
	name     string
	contract InputContract
}

type requestBindingLeaf interface {
	requestBindingMetadata() bindingMetadata
}

type bodyObjectMetadata struct {
	descriptor any
	options    []InputOption
}

type requestBodyObject interface {
	requestBodyObjectMetadata() bodyObjectMetadata
}

func (binding Binding[T]) requestBindingMetadata() bindingMetadata {
	contract := binding.contract
	if contract.GoType == nil {
		contract.GoType = reflect.TypeFor[T]()
	}
	return bindingMetadata{
		source:   binding.source,
		name:     binding.name,
		contract: contract,
	}
}

func newBinding[T any](source BindingSource, name string, options ...InputOption) Binding[T] {
	return Binding[T]{
		source:   source,
		name:     name,
		contract: InputOf[T](options...),
	}
}

// Query binds one query parameter to the corresponding request field.
func Query[T any](name string, options ...InputOption) Binding[T] {
	return newBinding[T](BindingSourceQuery, name, options...)
}

// Path binds one path parameter to the corresponding request field. Path
// parameters are always required.
func Path[T any](name string, options ...InputOption) Binding[T] {
	options = append([]InputOption{Required()}, options...)
	return newBinding[T](BindingSourcePath, name, options...)
}

// Header binds one request header to the corresponding request field.
func Header[T any](name string, options ...InputOption) Binding[T] {
	return newBinding[T](BindingSourceHeader, name, options...)
}

// Cookie binds one request cookie to the corresponding request field.
func Cookie[T any](name string, options ...InputOption) Binding[T] {
	return newBinding[T](BindingSourceCookie, name, options...)
}

// Value describes a field inside a BodyObject. It inherits the enclosing body
// source and adds typed rules at the corresponding request field.
func Value[T any](options ...InputOption) Binding[T] {
	return newBinding[T](BindingSourceValue, "", options...)
}

// BodyValue is an explicit spelling of Value.
func BodyValue[T any](options ...InputOption) Binding[T] {
	return Value[T](options...)
}

// Body decodes the complete transport body into one request field. It can also
// represent the complete operation request when used as the contract root.
func Body[T any](options ...InputOption) Binding[T] {
	return newBinding[T](BindingSourceBody, "", options...)
}

// BodyObjectBinding is a companion node whose target subtree is decoded from
// the request body. Its descendant leaves use Value[T] and may be nested in
// ordinary companion structs to describe deeply nested body fields.
type BodyObjectBinding[T any] struct {
	descriptor T
	options    []InputOption
}

// BodyObject declares that the complete request body maps to the corresponding
// request subtree, while descriptor supplies recursive metadata for values in
// that subtree.
func BodyObject[T any](descriptor T, options ...InputOption) BodyObjectBinding[T] {
	return BodyObjectBinding[T]{
		descriptor: descriptor,
		options:    append([]InputOption(nil), options...),
	}
}

func (object BodyObjectBinding[T]) requestBodyObjectMetadata() bodyObjectMetadata {
	return bodyObjectMetadata{
		descriptor: object.descriptor,
		options:    append([]InputOption(nil), object.options...),
	}
}

// RequestBinding is the compiled form consumed by transports and generated
// invocation plans. FieldIndex follows the real request type (not the
// companion type), while FieldPath is retained for deterministic diagnostics.
// API authors normally construct the recursive OperationContract.Request
// descriptor instead of creating RequestBinding values directly.
type RequestBinding struct {
	Source     BindingSource
	Name       string
	FieldPath  []string
	FieldIndex []int
	Input      InputContract
}

// normalizeRequestBindings validates a request companion against the actual
// operation input type and flattens its binding leaves. A nil companion means
// the operation does not opt into structural request binding.
func normalizeRequestBindings(request any, inputType reflect.Type) ([]RequestBinding, error) {
	if request == nil {
		return nil, nil
	}
	if inputType == nil {
		return nil, fmt.Errorf("request contract is declared but the operation has no request argument")
	}

	walker := requestBindingWalker{
		activePointers: make(map[uintptr]string),
		wireBindings:   make(map[string]string),
	}
	if err := walker.walk(reflect.ValueOf(request), inputType, nil, nil, false); err != nil {
		return nil, err
	}
	if len(walker.bindings) == 0 {
		return nil, fmt.Errorf("request contract contains no bindings")
	}
	return walker.bindings, nil
}

type requestBindingWalker struct {
	bindings       []RequestBinding
	bodyPath       string
	activePointers map[uintptr]string
	wireBindings   map[string]string
}

func (walker *requestBindingWalker) walk(
	descriptor reflect.Value,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	insideBody bool,
) error {
	path := displayRequestFieldPath(fieldPath)
	if !descriptor.IsValid() {
		return fmt.Errorf("request contract field %q is invalid", path)
	}

	if leaf, ok := requestBindingLeafValue(descriptor); ok {
		return walker.addBinding(leaf.requestBindingMetadata(), targetType, fieldPath, fieldIndex, insideBody)
	}
	if object, ok := requestBodyObjectValue(descriptor); ok {
		return walker.addBodyObject(object.requestBodyObjectMetadata(), targetType, fieldPath, fieldIndex)
	}

	for descriptor.Kind() == reflect.Pointer {
		if descriptor.IsNil() {
			return fmt.Errorf("request contract field %q is a nil pointer", path)
		}
		pointer := descriptor.Pointer()
		if previous, cyclic := walker.activePointers[pointer]; cyclic {
			return fmt.Errorf("request contract field %q forms a pointer cycle through %q", path, previous)
		}
		walker.activePointers[pointer] = path
		defer delete(walker.activePointers, pointer)
		descriptor = descriptor.Elem()
	}

	if descriptor.Kind() != reflect.Struct {
		return fmt.Errorf(
			"request contract field %q has type %s; want a companion struct or Binding[T]",
			path,
			descriptor.Type(),
		)
	}

	targetStruct := targetType
	for targetStruct.Kind() == reflect.Pointer {
		targetStruct = targetStruct.Elem()
	}
	if targetStruct.Kind() != reflect.Struct {
		return fmt.Errorf(
			"request contract field %q is a nested companion struct, but request field has type %s",
			path,
			targetType,
		)
	}

	descriptorType := descriptor.Type()
	for index := 0; index < descriptorType.NumField(); index++ {
		descriptorField := descriptorType.Field(index)
		childPath := appendRequestPath(fieldPath, descriptorField.Name)
		childPathText := displayRequestFieldPath(childPath)
		if !descriptorField.IsExported() {
			return fmt.Errorf("request contract field %q must be exported", childPathText)
		}

		targetField, exists := directStructField(targetStruct, descriptorField.Name)
		if !exists {
			return fmt.Errorf("request contract field %q does not exist in request type %s", childPathText, targetStruct)
		}
		if !targetField.IsExported() {
			return fmt.Errorf("request field %q must be exported", childPathText)
		}

		childIndex := appendRequestIndex(fieldIndex, targetField.Index...)
		if err := walker.walk(descriptor.Field(index), targetField.Type, childPath, childIndex, insideBody); err != nil {
			return err
		}
	}
	return nil
}

func (walker *requestBindingWalker) addBinding(
	metadata bindingMetadata,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	insideBody bool,
) error {
	path := displayRequestFieldPath(fieldPath)
	if metadata.source == "" {
		return fmt.Errorf("request contract field %q has an uninitialized Binding", path)
	}
	if metadata.contract.GoType != targetType {
		return fmt.Errorf(
			"request contract field %q binds %s, but request field has type %s",
			path,
			metadata.contract.GoType,
			targetType,
		)
	}

	switch metadata.source {
	case BindingSourceQuery, BindingSourcePath, BindingSourceHeader, BindingSourceCookie:
		if insideBody {
			return fmt.Errorf("request contract field %q cannot bind %s from inside a BodyObject", path, metadata.source)
		}
		if strings.TrimSpace(metadata.name) == "" {
			return fmt.Errorf("request contract field %q has an empty %s name", path, metadata.source)
		}
		if !requestScalarType(targetType) {
			return fmt.Errorf(
				"request contract field %q has type %s; %s bindings require a scalar",
				path,
				targetType,
				metadata.source,
			)
		}
		wireKey := requestWireBindingKey(metadata.source, metadata.name)
		if previous, duplicate := walker.wireBindings[wireKey]; duplicate {
			return fmt.Errorf(
				"request contract fields %q and %q both bind %s %q",
				previous,
				path,
				metadata.source,
				metadata.name,
			)
		}
		walker.wireBindings[wireKey] = path
	case BindingSourceBody:
		if insideBody {
			return fmt.Errorf("request contract field %q cannot declare a nested body binding", path)
		}
		if walker.bodyPath != "" {
			return fmt.Errorf("request contract fields %q and %q both bind the request body", walker.bodyPath, path)
		}
		walker.bodyPath = path
	case BindingSourceValue:
		if !insideBody {
			return fmt.Errorf("request contract field %q uses Value outside a BodyObject", path)
		}
	default:
		return fmt.Errorf("request contract field %q has unsupported binding source %q", path, metadata.source)
	}

	normalized, err := normalizeInputContract(metadata.contract)
	if err != nil {
		return fmt.Errorf("request contract field %q: %w", path, err)
	}
	walker.bindings = append(walker.bindings, RequestBinding{
		Source:     metadata.source,
		Name:       metadata.name,
		FieldPath:  append([]string(nil), fieldPath...),
		FieldIndex: append([]int(nil), fieldIndex...),
		Input:      normalized,
	})
	return nil
}

func (walker *requestBindingWalker) addBodyObject(
	metadata bodyObjectMetadata,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
) error {
	path := displayRequestFieldPath(fieldPath)
	if walker.bodyPath != "" {
		return fmt.Errorf("request contract fields %q and %q both bind the request body", walker.bodyPath, path)
	}
	targetStruct := targetType
	for targetStruct.Kind() == reflect.Pointer {
		targetStruct = targetStruct.Elem()
	}
	if targetStruct.Kind() != reflect.Struct {
		return fmt.Errorf("request contract field %q uses BodyObject for non-struct request field %s", path, targetType)
	}

	contract := InputContract{GoType: targetType}
	for _, option := range metadata.options {
		if option != nil {
			option.applyInput(&contract)
		}
	}
	normalized, err := normalizeInputContract(contract)
	if err != nil {
		return fmt.Errorf("request contract body object %q: %w", path, err)
	}

	walker.bodyPath = path
	walker.bindings = append(walker.bindings, RequestBinding{
		Source:     BindingSourceBody,
		FieldPath:  append([]string(nil), fieldPath...),
		FieldIndex: append([]int(nil), fieldIndex...),
		Input:      normalized,
	})
	return walker.walk(reflect.ValueOf(metadata.descriptor), targetType, fieldPath, fieldIndex, true)
}

func requestBindingLeafValue(value reflect.Value) (requestBindingLeaf, bool) {
	if !value.IsValid() || !value.CanInterface() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return nil, false
	}
	leaf, ok := value.Interface().(requestBindingLeaf)
	return leaf, ok
}

func requestBodyObjectValue(value reflect.Value) (requestBodyObject, bool) {
	if !value.IsValid() || !value.CanInterface() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return nil, false
	}
	object, ok := value.Interface().(requestBodyObject)
	return object, ok
}

func directStructField(structType reflect.Type, name string) (reflect.StructField, bool) {
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		if field.Name == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func requestScalarType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return httpPrimitiveScalarKind(valueType.Kind())
}

func requestWireBindingKey(source BindingSource, name string) string {
	if source == BindingSourceHeader {
		name = strings.ToLower(name)
	}
	return string(source) + "\x00" + name
}

func appendRequestPath(path []string, field string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, field)
}

func appendRequestIndex(index []int, fields ...int) []int {
	result := make([]int, len(index), len(index)+len(fields))
	copy(result, index)
	return append(result, fields...)
}

func displayRequestFieldPath(path []string) string {
	if len(path) == 0 {
		return "<request>"
	}
	return strings.Join(path, ".")
}
