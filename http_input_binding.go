package koba

import (
	"encoding"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

// bindHTTPInputs is the HTTP adapter half of the structural contract. It maps
// HTTP values to semantic identifiers and coerces them to their declared Go
// types. Defaults and constraints are then applied by the transport-neutral
// validator.
func bindHTTPInputs(operation OperationDescription, c *httprouter.Context) (OperationInputs, error) {
	if len(operation.Contract.Inputs) == 0 {
		return OperationInputs{}, nil
	}
	supplied := make(map[string]any, len(operation.Contract.Inputs))
	for _, parameter := range operation.Documentation.Parameters {
		if parameter.ContractInput == "" {
			continue
		}
		raw, present, err := httpParameterValue(c.Request, c.Params, parameter)
		if err != nil {
			return OperationInputs{}, err
		}
		if !present {
			continue
		}
		input := operation.Contract.Inputs[parameter.ContractInput]
		value, err := coerceHTTPScalar(raw, input.GoType)
		if err != nil {
			return OperationInputs{}, fmt.Errorf(
				"%s parameter %q: %w",
				parameter.In,
				parameter.Name,
				err,
			)
		}
		supplied[parameter.ContractInput] = value
	}
	return ValidateOperationInputs(operation.Contract, supplied)
}

func httpParameterValue(request *http.Request, path httprouter.Params, parameter ParameterDocumentation) (string, bool, error) {
	var values []string
	switch parameter.In {
	case ParameterInQuery:
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			return "", false, fmt.Errorf("query parameters are malformed: %w", err)
		}
		values = query[parameter.Name]
	case ParameterInHeader:
		values = request.Header.Values(parameter.Name)
	case ParameterInCookie:
		for _, cookie := range request.Cookies() {
			if cookie.Name == parameter.Name {
				values = append(values, cookie.Value)
			}
		}
	case ParameterInPath:
		value, exists := path.Get(parameter.Name)
		if exists {
			values = []string{value}
		}
	default:
		return "", false, fmt.Errorf("parameter %q has unsupported HTTP location %q", parameter.Name, parameter.In)
	}
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) > 1 {
		return "", false, fmt.Errorf("%s parameter %q must contain exactly one value", parameter.In, parameter.Name)
	}
	return values[0], true, nil
}

func coerceHTTPScalar(raw string, target reflect.Type) (any, error) {
	if target == nil {
		return nil, fmt.Errorf("has no target Go type")
	}
	if target.Kind() == reflect.Pointer {
		value, err := coerceHTTPScalar(raw, target.Elem())
		if err != nil {
			return nil, err
		}
		pointer := reflect.New(target.Elem())
		pointer.Elem().Set(reflect.ValueOf(value))
		if pointer.Type().AssignableTo(target) {
			return pointer.Interface(), nil
		}
		if pointer.Type().ConvertibleTo(target) {
			return pointer.Convert(target).Interface(), nil
		}
		return nil, fmt.Errorf("type %s does not support HTTP scalar decoding", target)
	}
	if !httpPrimitiveScalarKind(target.Kind()) {
		return nil, fmt.Errorf("type %s does not support HTTP scalar decoding", target)
	}
	if pointer := reflect.New(target); pointer.Type().Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		unmarshaler := pointer.Interface().(encoding.TextUnmarshaler)
		if err := unmarshaler.UnmarshalText([]byte(raw)); err != nil {
			return nil, fmt.Errorf("is invalid: %w", err)
		}
		return pointer.Elem().Interface(), nil
	}
	value := reflect.New(target).Elem()
	switch target.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("must be a boolean")
		}
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(raw, 10, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("must be a %s", target)
		}
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(raw, 10, target.Bits())
		if err != nil {
			return nil, fmt.Errorf("must be a %s", target)
		}
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(raw, target.Bits())
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return nil, fmt.Errorf("must be a finite %s", target)
		}
		value.SetFloat(parsed)
	default:
		return nil, fmt.Errorf("type %s does not support HTTP scalar decoding", target)
	}
	return value.Interface(), nil
}

func validateHTTPScalarType(target reflect.Type) error {
	if target == nil {
		return fmt.Errorf("HTTP-bound input has no Go type")
	}
	if httpPrimitiveScalarKind(target.Kind()) {
		return nil
	}
	return fmt.Errorf("contract input type %s does not support HTTP scalar decoding", target)
}

func httpPrimitiveScalarKind(kind reflect.Kind) bool {
	return kind == reflect.String || kind == reflect.Bool || numericKind(kind)
}
