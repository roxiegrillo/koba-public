package koba

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	)

// constructOperationRequest materializes the request argument declared by an
// API function. The compiled bindings are the only bridge between HTTP values
// and the request fields; the API function receives an ordinary typed value.
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
	if hasBody {
		decoded := reflect.New(body.Input.GoType)
		err := c.ShouldBindBodyWith(decoded.Interface(), httprouter.BindingJSON)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, requestBindingError(body, err)
		}
		if raw, exists := c.Get(httprouter.BodyBytesKey); exists {
			if encoded, ok := raw.([]byte); ok && len(bytes.TrimSpace(encoded)) != 0 {
				if err := json.Unmarshal(encoded, &bodyDocument); err != nil {
					return nil, requestBindingError(body, err)
				}
				if bodyDocument == nil {
					return nil, requestBindingError(body, fmt.Errorf("must not be null"))
				}
				bodyPresent = true
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
		} else if body.Input.HasDefault {
			request, err = assignDecodedBody(request, operation.InputType, body, reflect.ValueOf(body.Input.Default))
			if err != nil {
				return nil, requestBindingError(body, err)
			}
		}
	}

	for _, binding := range operation.RequestBindings {
		switch binding.Source {
		case BindingSourceBody:
			if bodyPresent {
				target, err := requestFieldValue(request, operation.InputType, binding.FieldIndex, false)
				if err != nil {
					return nil, requestBindingError(binding, err)
				}
				if err := validateRequestField(binding, target); err != nil {
					return nil, err
				}
			}
		case BindingSourceValue:
			if !bodyPresent {
				continue
			}
			present, null, err := bodyFieldPresence(
				bodyDocument,
				operation.InputType,
				body.FieldPath,
				binding.FieldPath,
			)
			if err != nil {
				return nil, requestBindingError(binding, err)
			}
			if null {
				return nil, requestBindingError(binding, fmt.Errorf("must not be null"))
			}
			if err := populateBodyValue(request, operation.InputType, binding, present); err != nil {
				return nil, err
			}
		case BindingSourceQuery, BindingSourcePath, BindingSourceHeader, BindingSourceCookie:
			if err := populateTransportValue(request, operation.InputType, binding, c); err != nil {
				return nil, err
			}
		default:
			return nil, requestBindingError(binding, fmt.Errorf("unsupported source %q", binding.Source))
		}
	}
	return request.Interface(), nil
}

func newRequestValue(inputType reflect.Type) reflect.Value {
	return reflect.New(inputType).Elem()
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
	value, err := coerceHTTPScalar(raw, binding.Input.GoType)
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
) error {
	if !present {
		return populateMissingRequestField(request, inputType, binding)
	}
	target, err := requestFieldValue(request, inputType, binding.FieldIndex, false)
	if err != nil {
		return requestBindingError(binding, err)
	}
	return validateRequestField(binding, target)
}

func populateMissingRequestField(
	request reflect.Value,
	inputType reflect.Type,
	binding RequestBinding,
) error {
	if binding.Input.HasDefault {
		return setRequestField(request, inputType, binding, binding.Input.Default)
	}
	if binding.Input.Required {
		return requestBindingError(binding, fmt.Errorf("is required"))
	}
	return nil
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
	if document == nil {
		return false, false, nil
	}
	if len(fieldPath) < len(bodyPath) {
		return false, false, fmt.Errorf("field is outside the body subtree")
	}
	for index := range bodyPath {
		if bodyPath[index] != fieldPath[index] {
			return false, false, fmt.Errorf("field is outside the body subtree")
		}
	}
	relative := fieldPath[len(bodyPath):]
	bodyType, err := requestTypeAtPath(inputType, bodyPath)
	if err != nil {
		return false, false, err
	}
	current := document
	currentType := bodyType
	for _, fieldName := range relative {
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
		if currentType.Kind() != reflect.Struct {
			return false, false, fmt.Errorf("body field parent %s is not a struct", currentType)
		}
		field, exists := directStructField(currentType, fieldName)
		if !exists {
			return false, false, fmt.Errorf("body field %q does not exist", fieldName)
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			return false, false, fmt.Errorf("body field %q is excluded from JSON", fieldName)
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		object, ok := current.(map[string]any)
		if !ok {
			return false, false, nil
		}
		value, exists := jsonObjectValue(object, jsonName)
		if !exists {
			return false, false, nil
		}
		if value == nil {
			return true, true, nil
		}
		current = value
		currentType = field.Type
	}
	return true, current == nil, nil
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

func jsonObjectValue(object map[string]any, name string) (any, bool) {
	if value, exists := object[name]; exists {
		return value, true
	}
	for key, value := range object {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return nil, false
}
