package koba

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"

	internalauthorization "github.com/roxiegrillo/koba/v2/internal/authorization"
	internalhttpscalar "github.com/roxiegrillo/koba/v2/internal/transport/httpscalar"
)

// constructOperationRequest materializes the request argument declared by an
// API function. The compiled bindings are the only bridge from adapter and
// authenticated-context values into request fields; the API function receives
// an ordinary typed value.
func constructOperationRequest(operation OperationDescription, c *httprouter.Context) (any, error) {
	if len(operation.RequestBindings) == 0 {
		return nil, nil
	}
	if operation.InputType == nil {
		return nil, fmt.Errorf("request bindings require an operation input type")
	}

	request := newRequestValue(operation.InputType)
	body, hasBody := operation.BodyBinding()
	var bodyDocument any
	bodyPresent := false
	bodyEffective := false
	if hasBody {
		decoded := reflect.New(body.Input.GoType)
		encoded, err := readRequestBody(c)
		if err != nil {
			return nil, requestBindingError(body, err)
		}
		if len(bytes.TrimSpace(encoded)) != 0 {
			if err := json.Unmarshal(encoded, &bodyDocument); err != nil {
				return nil, requestBindingError(body, err)
			}
			if bodyDocument == nil && !body.Input.Nullable {
				return nil, requestBindingError(body, fmt.Errorf("must not be null"))
			}
			bodyPresent = true
		}
		if bodyPresent && bodyDocument != nil {
			err = c.ShouldBindBodyWith(decoded.Interface(), httprouter.BindingJSON)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, requestBindingError(body, err)
			}
		}
		if !bodyPresent && body.Input.Required {
			return nil, requestBindingError(body, fmt.Errorf("is required"))
		}
		if bodyPresent {
			request, err = assignDecodedBody(request, operation.InputType, body, decoded.Elem())
			if err != nil {
				return nil, requestBindingError(body, err)
			}
			bodyEffective = true
		} else if body.Input.HasDefault {
			request, err = assignDecodedBody(request, operation.InputType, body, reflect.ValueOf(appliedContractDefault(body.Input)))
			if err != nil {
				return nil, requestBindingError(body, err)
			}
			bodyDocument, err = contractDefaultDocument(body.Input)
			if err != nil {
				return nil, requestBindingError(body, err)
			}
			bodyEffective = true
		}
	}

	for _, binding := range operation.RequestBindings {
		switch binding.Source {
		case BindingSourceBody:
			if bodyEffective {
				target, err := requestFieldValue(request, operation.InputType, binding.FieldIndex, false)
				if err != nil {
					return nil, requestBindingError(binding, err)
				}
				if err := applyCollectionInputDefaults(binding.Input, bodyDocument, target); err != nil {
					return nil, requestBindingError(binding, err)
				}
				if err := validateRequestField(binding, target); err != nil {
					return nil, err
				}
				if err := validateCollectionInputDocument(binding.Input, bodyDocument); err != nil {
					return nil, requestBindingError(binding, err)
				}
			}
		case BindingSourceValue:
			if !bodyEffective || bodyDocument == nil {
				continue
			}
			fieldDocument, present, null, err := bodyFieldDocument(
				bodyDocument,
				operation.InputType,
				body.FieldPath,
				binding.FieldPath,
			)
			if err != nil {
				return nil, requestBindingError(binding, err)
			}
			if null && !binding.Input.Nullable {
				return nil, requestBindingError(binding, fmt.Errorf("must not be null"))
			}
			if err := populateBodyValue(request, operation.InputType, binding, present, fieldDocument); err != nil {
				return nil, err
			}
			if present && !null {
				if err := validateCollectionInputDocument(binding.Input, fieldDocument); err != nil {
					return nil, requestBindingError(binding, err)
				}
			}
		case BindingSourceQuery, BindingSourcePath, BindingSourceHeader, BindingSourceCookie:
			if err := populateTransportValue(request, operation.InputType, binding, c); err != nil {
				return nil, err
			}
		case BindingSourceSubject:
			if err := populateAuthenticatedSubject(request, operation.InputType, binding, c); err != nil {
				return nil, err
			}
		default:
			return nil, requestBindingError(binding, fmt.Errorf("unsupported source %q", binding.Source))
		}
	}
	if hasBody && bodyEffective && bodyDocument != nil {
		if err := validateBodyObjectRules(body, bodyDocument); err != nil {
			return nil, err
		}
	}
	return request.Interface(), nil
}

func contractDefaultDocument(input InputContract) (any, error) {
	encoded, err := json.Marshal(appliedContractDefault(input))
	if err != nil {
		return nil, fmt.Errorf("default cannot be represented as JSON: %w", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, fmt.Errorf("default JSON cannot be inspected: %w", err)
	}
	return document, nil
}

func validateRequestBodyDefault(bindings []RequestBinding, inputType reflect.Type) error {
	var body *RequestBinding
	for index := range bindings {
		if bindings[index].Source == BindingSourceBody && bindings[index].Input.HasDefault {
			body = &bindings[index]
			break
		}
	}
	if body == nil {
		return nil
	}
	bodyDocument, err := contractDefaultDocument(body.Input)
	if err != nil {
		return requestBindingError(*body, err)
	}
	request := newRequestValue(inputType)
	request, err = assignDecodedBody(
		request,
		inputType,
		*body,
		reflect.ValueOf(appliedContractDefault(body.Input)),
	)
	if err != nil {
		return requestBindingError(*body, err)
	}
	for _, binding := range bindings {
		switch binding.Source {
		case BindingSourceBody:
			target, targetErr := requestFieldValue(request, inputType, binding.FieldIndex, false)
			if targetErr != nil {
				return requestBindingError(binding, targetErr)
			}
			if targetErr = applyCollectionInputDefaults(binding.Input, bodyDocument, target); targetErr != nil {
				return requestBindingError(binding, targetErr)
			}
			if targetErr = validateRequestField(binding, target); targetErr != nil {
				return targetErr
			}
			if targetErr = validateCollectionInputDocument(binding.Input, bodyDocument); targetErr != nil {
				return requestBindingError(binding, targetErr)
			}
		case BindingSourceValue:
			fieldDocument, present, null, fieldErr := bodyFieldDocument(
				bodyDocument,
				inputType,
				body.FieldPath,
				binding.FieldPath,
			)
			if fieldErr != nil {
				return requestBindingError(binding, fieldErr)
			}
			if null && !binding.Input.Nullable {
				return requestBindingError(binding, fmt.Errorf("must not be null"))
			}
			if fieldErr = populateBodyValue(request, inputType, binding, present, fieldDocument); fieldErr != nil {
				return fieldErr
			}
			if present && !null {
				if fieldErr = validateCollectionInputDocument(binding.Input, fieldDocument); fieldErr != nil {
					return requestBindingError(binding, fieldErr)
				}
			}
		}
	}
	if bodyDocument != nil {
		if err := validateBodyObjectRules(*body, bodyDocument); err != nil {
			return err
		}
	}
	return nil
}

// readRequestBody inspects the JSON document before Gin binds it. This is
// necessary for nullable pointer bodies because Gin's legacy validator cannot
// validate a nil pointer decoded from the JSON literal null. The body is
// restored so the normal Gin binding and validation path remains unchanged for
// every non-null document.
func readRequestBody(c *httprouter.Context) ([]byte, error) {
	encoded, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(encoded))
	return encoded, nil
}

func newRequestValue(inputType reflect.Type) reflect.Value {
	return reflect.New(inputType).Elem()
}

func populateAuthenticatedSubject(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
	c *httprouter.Context,
) error {
	subject, err := internalauthorization.Subject(c.Request.Context())
	if err != nil {
		return requestBindingError(binding, err)
	}
	if err := validateInputValue(binding.Input, subject); err != nil {
		return requestBindingError(binding, err)
	}
	return setRequestField(request, inputType, binding, subject)
}

func populateTransportValue(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
	c *httprouter.Context,
) error {
	parameter := ParameterDocumentation{Name: binding.Name, In: ParameterLocation(binding.Source)}
	raw, present, err := httpParameterValue(c.Request, c.Params, parameter)
	if err != nil {
		return requestBindingError(binding, err)
	}
	if !present {
		return populateMissingRequestField(request, inputType, binding)
	}
	value, err := internalhttpscalar.Decode(raw, binding.Input.GoType)
	if err != nil {
		return requestBindingError(binding, err)
	}
	if err := validateInputValue(binding.Input, value); err != nil {
		return requestBindingError(binding, err)
	}
	return setRequestField(request, inputType, binding, value)
}

func populateBodyValue(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
	present bool,
	document any,
) error {
	if !present {
		return populateMissingRequestField(request, inputType, binding)
	}
	target, err := requestFieldValue(request, inputType, binding.FieldIndex, false)
	if err != nil {
		return requestBindingError(binding, err)
	}
	if err := applyCollectionInputDefaults(binding.Input, document, target); err != nil {
		return requestBindingError(binding, err)
	}
	return validateRequestField(binding, target)
}

func applyCollectionInputDefaults(input InputContract, document any, target reflect.Value) error {
	if input.Items == nil || document == nil {
		return nil
	}
	values, ok := document.([]any)
	if !ok {
		return nil
	}
	for target.Kind() == reflect.Interface || target.Kind() == reflect.Pointer {
		if target.IsNil() {
			return nil
		}
		target = target.Elem()
	}
	if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
		return nil
	}
	if len(values) != target.Len() {
		return fmt.Errorf("decoded collection length %d does not match JSON length %d", target.Len(), len(values))
	}
	for index, documentElement := range values {
		if documentElement == nil {
			continue
		}
		if err := applyCollectionElementDefaults(
			input.Items.Input.GoType,
			input.Items.Bindings,
			documentElement,
			target.Index(index),
		); err != nil {
			return fmt.Errorf("item %d: %w", index+1, err)
		}
	}
	return nil
}

func applyCollectionElementDefaults(
	elementType reflect.Type,
	bindings []ResponseBinding,
	document any,
	element reflect.Value,
) error {
	for _, binding := range bindings {
		valueDocument, present, null, err := collectionFieldDocument(document, elementType, binding.FieldPath)
		path := displayResponseFieldPath(binding.FieldPath)
		if err != nil {
			return fmt.Errorf("field %q: %w", path, err)
		}
		if !present {
			if !binding.Input.HasDefault {
				continue
			}
			if err := setCollectionElementField(element, binding, appliedContractDefault(binding.Input)); err != nil {
				return fmt.Errorf("field %q: %w", path, err)
			}
			continue
		}
		if null || binding.Input.Items == nil {
			continue
		}
		target, err := collectionElementFieldTarget(element, binding.FieldIndex, false)
		if err != nil {
			return fmt.Errorf("field %q: %w", path, err)
		}
		if err := applyCollectionInputDefaults(binding.Input, valueDocument, target); err != nil {
			return fmt.Errorf("field %q: %w", path, err)
		}
	}
	return nil
}

func setCollectionElementField(element reflect.Value, binding ResponseBinding, value any) error {
	target, err := collectionElementFieldTarget(element, binding.FieldIndex, true)
	if err != nil {
		return err
	}
	source := reflect.ValueOf(value)
	if !source.IsValid() || !source.Type().AssignableTo(target.Type()) {
		return fmt.Errorf("default has type %T, want %s", value, target.Type())
	}
	target.Set(source)
	return nil
}

func collectionElementFieldTarget(
	element reflect.Value,
	index []int,
	allocate bool,
) (reflect.Value, error) {
	current := element
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Interface || current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if !allocate || current.Kind() != reflect.Pointer || !current.CanSet() {
					return reflect.Value{}, fmt.Errorf("parent object is absent")
				}
				current.Set(reflect.New(current.Type().Elem()))
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return reflect.Value{}, fmt.Errorf("field index %v is invalid", index[:position+1])
		}
		current = current.Field(fieldIndex)
	}
	if !current.IsValid() || !current.CanSet() {
		return reflect.Value{}, fmt.Errorf("field is not settable")
	}
	return current, nil
}

func populateMissingRequestField(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
) error {
	if binding.Input.HasDefault {
		return setRequestField(request, inputType, binding, appliedContractDefault(binding.Input))
	}
	if binding.Input.Required {
		return requestBindingError(binding, fmt.Errorf("is required"))
	}
	return nil
}

func appliedContractDefault(input InputContract) any {
	value, err := cloneDeterministicContractValue(input.Default)
	if err != nil {
		// Contract normalization has already validated every default. Keep this
		// invariant explicit without adding an impossible runtime error branch to
		// every request-population path.
		panic(fmt.Sprintf("koba: invalid normalized default: %v", err))
	}
	return value
}

func validateRequestField(binding RequestBinding, field reflect.Value) error {
	if !field.IsValid() {
		return requestBindingError(binding, fmt.Errorf("cannot be resolved"))
	}
	if err := validateInputValue(binding.Input, field.Interface()); err != nil {
		return requestBindingError(binding, err)
	}
	return nil
}

func setRequestField(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
	value any,
) error {
	target, err := requestFieldValue(request, inputType, binding.FieldIndex, true)
	if err != nil {
		return requestBindingError(binding, err)
	}
	source := reflect.ValueOf(value)
	if !source.IsValid() || !source.Type().AssignableTo(target.Type()) {
		return requestBindingError(binding, fmt.Errorf("value has type %T, want %s", value, target.Type()))
	}
	target.Set(source)
	return nil
}

func requestFieldValue(
	request reflect.Value,
	inputType reflect.Type,
	index []int,
	allocate bool,
) (reflect.Value, error) {
	if len(index) == 0 {
		if request.Type() != inputType || !request.CanSet() {
			return reflect.Value{}, fmt.Errorf("request root is not settable as %s", inputType)
		}
		return request, nil
	}
	current := request
	for position, fieldIndex := range index {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if !allocate || !current.CanSet() {
					return reflect.Value{}, fmt.Errorf("parent object is absent")
				}
				current.Set(reflect.New(current.Type().Elem()))
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct || fieldIndex < 0 || fieldIndex >= current.NumField() {
			return reflect.Value{}, fmt.Errorf("field index %v is invalid", index[:position+1])
		}
		current = current.Field(fieldIndex)
	}
	if !current.CanSet() {
		return reflect.Value{}, fmt.Errorf("field is not settable")
	}
	return current, nil
}

func assignDecodedBody(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
	decoded reflect.Value,
) (reflect.Value, error) {
	if len(binding.FieldIndex) == 0 {
		if !decoded.Type().AssignableTo(request.Type()) {
			return reflect.Value{}, fmt.Errorf("decoded body has type %s, want %s", decoded.Type(), request.Type())
		}
		request.Set(decoded)
		return request, nil
	}
	target, err := requestFieldValue(request, inputType, binding.FieldIndex, true)
	if err != nil {
		return reflect.Value{}, err
	}
	if !decoded.Type().AssignableTo(target.Type()) {
		return reflect.Value{}, fmt.Errorf("decoded body has type %s, want %s", decoded.Type(), target.Type())
	}
	target.Set(decoded)
	return request, nil
}

func requestBindingError(binding RequestBinding, cause error) error {
	return &InputValidationError{Input: displayRequestFieldPath(binding.FieldPath), Cause: cause}
}

func bodyFieldPresence(
	document any,
	inputType reflect.Type,
	bodyPath []string,
	fieldPath []string,
) (present bool, null bool, err error) {
	_, present, null, err = bodyFieldDocument(document, inputType, bodyPath, fieldPath)
	return present, null, err
}

func bodyFieldDocument(
	document any,
	inputType reflect.Type,
	bodyPath []string,
	fieldPath []string,
) (value any, present bool, null bool, err error) {
	if document == nil {
		return nil, false, false, nil
	}
	if len(fieldPath) < len(bodyPath) {
		return nil, false, false, fmt.Errorf("field is outside the body subtree")
	}
	for index := range bodyPath {
		if bodyPath[index] != fieldPath[index] {
			return nil, false, false, fmt.Errorf("field is outside the body subtree")
		}
	}
	relative := fieldPath[len(bodyPath):]
	bodyType, err := requestTypeAtPath(inputType, bodyPath)
	if err != nil {
		return nil, false, false, err
	}
	current := document
	currentType := bodyType
	for _, fieldName := range relative {
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
		if currentType.Kind() != reflect.Struct {
			return nil, false, false, fmt.Errorf("body field parent %s is not a struct", currentType)
		}
		field, exists := directStructField(currentType, fieldName)
		if !exists {
			return nil, false, false, fmt.Errorf("body field %q does not exist", fieldName)
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			return nil, false, false, fmt.Errorf("body field %q is excluded from JSON", fieldName)
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false, false, nil
		}
		value, exists, err := jsonObjectValue(object, jsonName)
		if err != nil {
			return nil, false, false, err
		}
		if !exists {
			return nil, false, false, nil
		}
		if value == nil {
			return nil, true, true, nil
		}
		current = value
		currentType = field.Type
	}
	return current, true, current == nil, nil
}

func validateCollectionInputDocument(input InputContract, document any) error {
	if input.Items == nil || document == nil {
		return nil
	}
	values, ok := document.([]any)
	if !ok {
		// JSON decoding and the typed request schema report shape errors before
		// collection metadata is evaluated.
		return nil
	}
	for index, element := range values {
		if element == nil {
			if !input.Items.Input.Nullable {
				return fmt.Errorf("item %d: must not be null", index+1)
			}
			continue
		}
		for _, binding := range input.Items.Bindings {
			value, present, null, err := collectionFieldDocument(
				element,
				input.Items.Input.GoType,
				binding.FieldPath,
			)
			path := displayResponseFieldPath(binding.FieldPath)
			if err != nil {
				return fmt.Errorf("item %d field %q: %w", index+1, path, err)
			}
			if !present {
				if binding.Input.Required {
					return fmt.Errorf("item %d field %q: is required", index+1, path)
				}
				continue
			}
			if null {
				if !binding.Input.Nullable {
					return fmt.Errorf("item %d field %q: must not be null", index+1, path)
				}
				continue
			}
			if err := validateCollectionInputDocument(binding.Input, value); err != nil {
				return fmt.Errorf("item %d field %q: %w", index+1, path, err)
			}
		}
	}
	return nil
}

func collectionFieldDocument(
	document any,
	elementType reflect.Type,
	fieldPath []string,
) (value any, present bool, null bool, err error) {
	current := document
	currentType := elementType
	for _, fieldName := range fieldPath {
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
		if currentType.Kind() != reflect.Struct {
			return nil, false, false, fmt.Errorf("parent %s is not a struct", currentType)
		}
		field, exists := directStructField(currentType, fieldName)
		if !exists {
			return nil, false, false, fmt.Errorf("field does not exist")
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			return nil, false, false, fmt.Errorf("field is excluded from JSON")
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false, false, nil
		}
		current, exists, err = jsonObjectValue(object, jsonName)
		if err != nil {
			return nil, false, false, err
		}
		if !exists {
			return nil, false, false, nil
		}
		if current == nil {
			return nil, true, true, nil
		}
		currentType = field.Type
	}
	return current, true, current == nil, nil
}

func requestTypeAtPath(inputType reflect.Type, path []string) (reflect.Type, error) {
	current := inputType
	for _, fieldName := range path {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return nil, fmt.Errorf("request field parent %s is not a struct", current)
		}
		field, exists := directStructField(current, fieldName)
		if !exists {
			return nil, fmt.Errorf("request field %q does not exist", fieldName)
		}
		current = field.Type
	}
	return current, nil
}

func jsonObjectValue(object map[string]any, name string) (any, bool, error) {
	value, exists := object[name]
	aliases := make([]string, 0, 1)
	for key := range object {
		if key != name && strings.EqualFold(key, name) {
			aliases = append(aliases, key)
		}
	}
	if len(aliases) != 0 {
		sort.Strings(aliases)
		return nil, false, fmt.Errorf(
			"property %q must use canonical JSON name %q",
			aliases[0],
			name,
		)
	}
	return value, exists, nil
}
