package koba

import (
	"fmt"
	"reflect"
	"strings"
)

// ResponseValueBinding describes one field in an operation's successful
// response. T must exactly match the corresponding response field type.
type ResponseValueBinding[T any] struct {
	contract      InputContract
	documentation *BindingDocumentation
}

// MessageValueBinding describes a scalar field in a directional socket
// message. It shares the same recursive schema machinery as successful
// responses while retaining a transport-neutral authoring name.
type MessageValueBinding[T any] struct {
	contract      InputContract
	documentation *BindingDocumentation
}

type responseValueMetadata struct {
	contract      InputContract
	documentation *BindingDocumentation
}

type responseBindingLeaf interface {
	responseBindingMetadata() responseValueMetadata
}

func (binding ResponseValueBinding[T]) responseBindingMetadata() responseValueMetadata {
	contract := binding.contract
	if contract.GoType == nil {
		contract.GoType = reflect.TypeFor[T]()
	}
	return responseValueMetadata{
		contract:      contract,
		documentation: cloneBindingDocumentation(binding.documentation),
	}
}

func (binding MessageValueBinding[T]) responseBindingMetadata() responseValueMetadata {
	contract := binding.contract
	if contract.GoType == nil {
		contract.GoType = reflect.TypeFor[T]()
	}
	return responseValueMetadata{
		contract:      contract,
		documentation: cloneBindingDocumentation(binding.documentation),
	}
}

// ResponseValue creates structural and documentation metadata for one
// successful-response field.
func ResponseValue[T any](options ...InputOption) ResponseValueBinding[T] {
	return ResponseValueBinding[T]{contract: InputOf[T](options...)}
}

// MessageValue creates structural and documentation metadata for one field in
// a directional socket message descriptor.
func MessageValue[T any](options ...InputOption) MessageValueBinding[T] {
	return MessageValueBinding[T]{contract: InputOf[T](options...)}
}

// Document attaches human-facing prose and an optional example to a response
// field.
func (binding ResponseValueBinding[T]) Document(
	description string,
	options ...BindingDocumentationOption,
) ResponseValueBinding[T] {
	documentation := BindingDocumentation{Description: description}
	for _, option := range options {
		if option != nil {
			option.applyBindingDocumentation(&documentation)
		}
	}
	binding.documentation = &documentation
	return binding
}

// Document attaches human-facing prose and an optional example to a socket
// message field.
func (binding MessageValueBinding[T]) Document(
	description string,
	options ...BindingDocumentationOption,
) MessageValueBinding[T] {
	documentation := BindingDocumentation{Description: description}
	for _, option := range options {
		if option != nil {
			option.applyBindingDocumentation(&documentation)
		}
	}
	binding.documentation = &documentation
	return binding
}

// ResponseObjectBinding describes an object response field and optionally a
// partial nested companion for its fields.
type ResponseObjectBinding[T any] struct {
	descriptor    T
	options       []InputOption
	documentation *BindingDocumentation
}

type responseObjectMetadata struct {
	descriptor    any
	options       []InputOption
	documentation *BindingDocumentation
}

type responseBindingObject interface {
	responseObjectMetadata() responseObjectMetadata
}

type responseListMetadata struct {
	descriptor    any
	contract      InputContract
	documentation *BindingDocumentation
}

type responseBindingList interface {
	responseListMetadata() responseListMetadata
}

// ResponseObject creates metadata for an object response field. descriptor is
// a partial companion struct whose exported fields mirror the response type.
func ResponseObject[T any](descriptor T, options ...InputOption) ResponseObjectBinding[T] {
	return ResponseObjectBinding[T]{
		descriptor: descriptor,
		options:    append([]InputOption(nil), options...),
	}
}

// Document attaches human-facing prose and an optional example to an object
// response field.
func (object ResponseObjectBinding[T]) Document(
	description string,
	options ...BindingDocumentationOption,
) ResponseObjectBinding[T] {
	documentation := BindingDocumentation{Description: description}
	for _, option := range options {
		if option != nil {
			option.applyBindingDocumentation(&documentation)
		}
	}
	object.documentation = &documentation
	return object
}

func (object ResponseObjectBinding[T]) responseObjectMetadata() responseObjectMetadata {
	return responseObjectMetadata{
		descriptor:    object.descriptor,
		options:       append([]InputOption(nil), object.options...),
		documentation: cloneBindingDocumentation(object.documentation),
	}
}

// ResponseListBinding describes a slice response field plus an optional
// partial companion for each element.
type ResponseListBinding[T any] struct {
	descriptor    any
	contract      InputContract
	documentation *BindingDocumentation
}

// ResponseList creates metadata for a []T response field. elementDescriptor
// mirrors T and may itself contain ResponseValue or ResponseObject leaves. It
// may be nil when only the collection itself needs metadata.
func ResponseList[T any](elementDescriptor any, options ...InputOption) ResponseListBinding[T] {
	return ResponseListBinding[T]{
		descriptor: elementDescriptor,
		contract:   InputOf[[]T](options...),
	}
}

// Document attaches human-facing prose and an optional example to a response
// list field.
func (list ResponseListBinding[T]) Document(
	description string,
	options ...BindingDocumentationOption,
) ResponseListBinding[T] {
	documentation := BindingDocumentation{Description: description}
	for _, option := range options {
		if option != nil {
			option.applyBindingDocumentation(&documentation)
		}
	}
	list.documentation = &documentation
	return list
}

func (list ResponseListBinding[T]) responseListMetadata() responseListMetadata {
	return responseListMetadata{
		descriptor:    list.descriptor,
		contract:      list.contract,
		documentation: cloneBindingDocumentation(list.documentation),
	}
}

// ResponseBinding is the compiled form consumed by renderers and generated
// plans. FieldPath and FieldIndex follow the real response type.
type ResponseBinding struct {
	FieldPath  []string
	FieldIndex []int
	// ElementAt contains field-path offsets after which traversal enters a
	// slice or array element. Zero represents a root response collection.
	ElementAt     []int
	Input         InputContract
	Documentation *BindingDocumentation
}

func normalizeResponseBindings(response any, outputType reflect.Type) ([]ResponseBinding, error) {
	if response == nil {
		return nil, nil
	}
	if outputType == nil {
		return nil, fmt.Errorf("response contract is declared but the operation has no response value")
	}
	walker := responseBindingWalker{activePointers: make(map[uintptr]string)}
	if err := walker.walk(reflect.ValueOf(response), outputType, nil, nil, nil); err != nil {
		return nil, err
	}
	if len(walker.bindings) == 0 {
		return nil, fmt.Errorf("response contract contains no bindings")
	}
	return walker.bindings, nil
}

func normalizeResolvedResponseBindings(
	bindings []ResponseBinding,
	outputType reflect.Type,
) ([]ResponseBinding, error) {
	return normalizeResolvedResponseBindingsWithState(
		bindings,
		outputType,
		make(map[*CollectionItemContract]struct{}),
	)
}

func normalizeResolvedResponseBindingsWithState(
	bindings []ResponseBinding,
	outputType reflect.Type,
	activeItems map[*CollectionItemContract]struct{},
) ([]ResponseBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	if outputType == nil {
		return nil, fmt.Errorf("response bindings require an operation output type")
	}
	result := make([]ResponseBinding, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		path := displayResponseFieldPath(binding.FieldPath)
		targetType, resolvedPath, err := responseTypeAndPathAtIndex(outputType, binding.FieldIndex, binding.ElementAt)
		if err != nil {
			return nil, fmt.Errorf("response binding %q: %w", path, err)
		}
		if !requestPathsEqual(resolvedPath, binding.FieldPath) {
			return nil, fmt.Errorf(
				"response binding %q has field index %v for %q",
				path,
				binding.FieldIndex,
				displayResponseFieldPath(resolvedPath),
			)
		}
		if targetType != binding.Input.GoType {
			return nil, fmt.Errorf(
				"response binding %q has type %s, but field has type %s",
				path,
				binding.Input.GoType,
				targetType,
			)
		}
		key := fmt.Sprintf("%v:%s", binding.ElementAt, strings.Join(binding.FieldPath, "\x00"))
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("response contract contains duplicate binding %q", path)
		}
		seen[key] = struct{}{}
		normalized, err := normalizeInputContractWithState(binding.Input, activeItems)
		if err != nil {
			return nil, fmt.Errorf("response binding %q: %w", path, err)
		}
		documentation, err := normalizeResponseDocumentation(binding.Documentation, normalized)
		if err != nil {
			return nil, fmt.Errorf("response binding %q: %w", path, err)
		}
		binding.Input = normalized
		binding.Documentation = documentation
		binding.FieldPath = append([]string(nil), binding.FieldPath...)
		binding.FieldIndex = append([]int(nil), binding.FieldIndex...)
		binding.ElementAt = append([]int(nil), binding.ElementAt...)
		result = append(result, binding)
	}
	return result, nil
}

func responseTypeAndPathAtIndex(
	responseType reflect.Type,
	index []int,
	elementAt []int,
) (reflect.Type, []string, error) {
	current := responseType
	path := make([]string, 0, len(index))
	elements := make(map[int]struct{}, len(elementAt))
	for _, offset := range elementAt {
		if offset < 0 || offset > len(index) {
			return nil, nil, fmt.Errorf("collection element offset %d is invalid for field index %v", offset, index)
		}
		if _, duplicate := elements[offset]; duplicate {
			return nil, nil, fmt.Errorf("duplicate collection element offset %d", offset)
		}
		elements[offset] = struct{}{}
	}
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if _, entersElement := elements[position]; entersElement {
			if current.Kind() != reflect.Slice && current.Kind() != reflect.Array {
				return nil, nil, fmt.Errorf("collection element offset %d targets non-list type %s", position, current)
			}
			current = current.Elem()
			for current.Kind() == reflect.Pointer {
				current = current.Elem()
			}
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return nil, nil, fmt.Errorf("field index %v is invalid for %s", index[:position+1], current)
		}
		field := current.Field(fieldIndex)
		path = append(path, field.Name)
		current = field.Type
	}
	if _, entersElement := elements[len(index)]; entersElement {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Slice && current.Kind() != reflect.Array {
			return nil, nil, fmt.Errorf("collection element offset %d targets non-list type %s", len(index), current)
		}
		current = current.Elem()
	}
	return current, path, nil
}

type responseBindingWalker struct {
	bindings       []ResponseBinding
	activePointers map[uintptr]string
}

func (walker *responseBindingWalker) walk(
	descriptor reflect.Value,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	path := displayResponseFieldPath(fieldPath)
	if !descriptor.IsValid() {
		return fmt.Errorf("response contract field %q is invalid", path)
	}
	if leaf, ok := responseBindingLeafValue(descriptor); ok {
		return walker.addBinding(leaf.responseBindingMetadata(), targetType, fieldPath, fieldIndex, elementAt)
	}
	if object, ok := responseBindingObjectValue(descriptor); ok {
		return walker.addObject(object.responseObjectMetadata(), targetType, fieldPath, fieldIndex, elementAt)
	}
	if list, ok := responseBindingListValue(descriptor); ok {
		return walker.addList(list.responseListMetadata(), targetType, fieldPath, fieldIndex, elementAt)
	}

	for descriptor.Kind() == reflect.Pointer {
		if descriptor.IsNil() {
			return fmt.Errorf("response contract field %q is a nil pointer", path)
		}
		pointer := descriptor.Pointer()
		if previous, cyclic := walker.activePointers[pointer]; cyclic {
			return fmt.Errorf("response contract field %q forms a pointer cycle through %q", path, previous)
		}
		walker.activePointers[pointer] = path
		defer delete(walker.activePointers, pointer)
		descriptor = descriptor.Elem()
	}

	targetStruct := targetType
	for targetStruct.Kind() == reflect.Pointer {
		targetStruct = targetStruct.Elem()
	}
	if descriptor.Kind() != reflect.Struct || targetStruct.Kind() != reflect.Struct {
		return fmt.Errorf(
			"response contract field %q has type %s; want a partial companion struct for response type %s",
			path,
			descriptor.Type(),
			targetType,
		)
	}
	return walker.walkStruct(descriptor, targetStruct, fieldPath, fieldIndex, elementAt)
}

func (walker *responseBindingWalker) walkStruct(
	descriptor reflect.Value,
	targetStruct reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	descriptorType := descriptor.Type()
	for index := 0; index < descriptorType.NumField(); index++ {
		descriptorField := descriptorType.Field(index)
		childPath := appendRequestPath(fieldPath, descriptorField.Name)
		childPathText := displayResponseFieldPath(childPath)
		if !descriptorField.IsExported() {
			return fmt.Errorf("response contract field %q must be exported", childPathText)
		}
		targetField, exists := directStructField(targetStruct, descriptorField.Name)
		if !exists {
			return fmt.Errorf("response contract field %q does not exist in response type %s", childPathText, targetStruct)
		}
		if !targetField.IsExported() {
			return fmt.Errorf("response field %q must be exported", childPathText)
		}
		childIndex := appendRequestIndex(fieldIndex, targetField.Index...)
		if err := walker.walk(descriptor.Field(index), targetField.Type, childPath, childIndex, elementAt); err != nil {
			return err
		}
	}
	return nil
}

func (walker *responseBindingWalker) addBinding(
	metadata responseValueMetadata,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	path := displayResponseFieldPath(fieldPath)
	if metadata.contract.GoType != targetType {
		return fmt.Errorf(
			"response contract field %q describes %s, but response field has type %s",
			path,
			metadata.contract.GoType,
			targetType,
		)
	}
	return walker.appendBinding(metadata.contract, metadata.documentation, fieldPath, fieldIndex, elementAt)
}

func (walker *responseBindingWalker) addObject(
	metadata responseObjectMetadata,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	targetStruct := targetType
	for targetStruct.Kind() == reflect.Pointer {
		targetStruct = targetStruct.Elem()
	}
	if targetStruct.Kind() != reflect.Struct {
		return fmt.Errorf("response contract object %q targets non-struct type %s", displayResponseFieldPath(fieldPath), targetType)
	}
	contract := InputContract{GoType: targetType}
	for _, option := range metadata.options {
		if option != nil {
			option.applyInput(&contract)
		}
	}
	if err := walker.appendBinding(contract, metadata.documentation, fieldPath, fieldIndex, elementAt); err != nil {
		return err
	}
	descriptor := reflect.ValueOf(metadata.descriptor)
	for descriptor.Kind() == reflect.Pointer {
		if descriptor.IsNil() {
			return fmt.Errorf("response contract object %q has a nil descriptor", displayResponseFieldPath(fieldPath))
		}
		descriptor = descriptor.Elem()
	}
	if descriptor.Kind() != reflect.Struct {
		return fmt.Errorf("response contract object %q descriptor must be a struct", displayResponseFieldPath(fieldPath))
	}
	return walker.walkStruct(descriptor, targetStruct, fieldPath, fieldIndex, elementAt)
}

func (walker *responseBindingWalker) addList(
	metadata responseListMetadata,
	targetType reflect.Type,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	if targetType != metadata.contract.GoType {
		return fmt.Errorf(
			"response contract list %q describes %s, but response field has type %s",
			displayResponseFieldPath(fieldPath),
			metadata.contract.GoType,
			targetType,
		)
	}
	if targetType.Kind() != reflect.Slice {
		return fmt.Errorf("response contract list %q targets non-list type %s", displayResponseFieldPath(fieldPath), targetType)
	}
	if err := walker.appendBinding(metadata.contract, metadata.documentation, fieldPath, fieldIndex, elementAt); err != nil {
		return err
	}
	if metadata.descriptor == nil {
		return nil
	}
	descriptor := reflect.ValueOf(metadata.descriptor)
	nextElements := append(append([]int(nil), elementAt...), len(fieldPath))
	if object, ok := responseBindingObjectValue(descriptor); ok {
		objectMetadata := object.responseObjectMetadata()
		contract := InputContract{GoType: targetType.Elem()}
		for _, option := range objectMetadata.options {
			if option != nil {
				option.applyInput(&contract)
			}
		}
		if err := walker.appendBinding(
			contract,
			objectMetadata.documentation,
			fieldPath,
			fieldIndex,
			nextElements,
		); err != nil {
			return err
		}
		descriptor = reflect.ValueOf(objectMetadata.descriptor)
	}
	for descriptor.Kind() == reflect.Pointer {
		if descriptor.IsNil() {
			return fmt.Errorf("response contract list %q has a nil element descriptor", displayResponseFieldPath(fieldPath))
		}
		descriptor = descriptor.Elem()
	}
	if descriptor.Kind() != reflect.Struct {
		return fmt.Errorf("response contract list %q element descriptor must be a struct", displayResponseFieldPath(fieldPath))
	}
	elementType := targetType.Elem()
	for elementType.Kind() == reflect.Pointer {
		elementType = elementType.Elem()
	}
	if elementType.Kind() != reflect.Struct {
		return fmt.Errorf(
			"response contract list %q element descriptor targets non-struct element type %s",
			displayResponseFieldPath(fieldPath),
			targetType.Elem(),
		)
	}
	return walker.walkStruct(descriptor, elementType, fieldPath, fieldIndex, nextElements)
}

func (walker *responseBindingWalker) appendBinding(
	contract InputContract,
	documentation *BindingDocumentation,
	fieldPath []string,
	fieldIndex []int,
	elementAt []int,
) error {
	normalized, err := normalizeInputContract(contract)
	if err != nil {
		return fmt.Errorf("response contract field %q: %w", displayResponseFieldPath(fieldPath), err)
	}
	documentation, err = normalizeResponseDocumentation(documentation, normalized)
	if err != nil {
		return fmt.Errorf("response contract field %q: %w", displayResponseFieldPath(fieldPath), err)
	}
	walker.bindings = append(walker.bindings, ResponseBinding{
		FieldPath:     append([]string(nil), fieldPath...),
		FieldIndex:    append([]int(nil), fieldIndex...),
		ElementAt:     append([]int(nil), elementAt...),
		Input:         normalized,
		Documentation: documentation,
	})
	return nil
}

func normalizeResponseDocumentation(
	documentation *BindingDocumentation,
	contract InputContract,
) (*BindingDocumentation, error) {
	if documentation == nil {
		return nil, nil
	}
	normalized := cloneBindingDocumentation(documentation)
	normalized.Description = strings.TrimSpace(normalized.Description)
	if normalized.Description == "" {
		return nil, fmt.Errorf("response documentation description is required")
	}
	if normalized.Example != nil {
		example, err := normalizeContractValue(normalized.Example, contract.GoType)
		if err != nil {
			return nil, fmt.Errorf("response documentation example: %w", err)
		}
		if isNilContractValue(example) {
			return nil, fmt.Errorf("response documentation example: must not be null")
		}
		if err := validateInputValue(contract, example); err != nil {
			return nil, fmt.Errorf("response documentation example: %w", err)
		}
		owned, err := cloneDocumentationContractValue(example)
		if err != nil {
			return nil, fmt.Errorf("response documentation example: %w", err)
		}
		normalized.Example = owned
	}
	return normalized, nil
}

func responseBindingLeafValue(value reflect.Value) (responseBindingLeaf, bool) {
	if !value.IsValid() || !value.CanInterface() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return nil, false
	}
	leaf, ok := value.Interface().(responseBindingLeaf)
	return leaf, ok
}

func responseBindingObjectValue(value reflect.Value) (responseBindingObject, bool) {
	if !value.IsValid() || !value.CanInterface() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return nil, false
	}
	object, ok := value.Interface().(responseBindingObject)
	return object, ok
}

func responseBindingListValue(value reflect.Value) (responseBindingList, bool) {
	if !value.IsValid() || !value.CanInterface() || (value.Kind() == reflect.Pointer && value.IsNil()) {
		return nil, false
	}
	list, ok := value.Interface().(responseBindingList)
	return list, ok
}

func displayResponseFieldPath(path []string) string {
	if len(path) == 0 {
		return "<response>"
	}
	return strings.Join(path, ".")
}
