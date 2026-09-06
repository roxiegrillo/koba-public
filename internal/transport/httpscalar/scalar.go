// Package httpscalar decodes primitive values at the HTTP transport boundary.
package httpscalar

import (
	"encoding"
	"fmt"
	"math"
	"reflect"
	"strconv"

	internalvaluekind "github.com/roxiegrillo/koba/v2/internal/valuekind"
)

// Decode converts one HTTP scalar into its declared Go type.
func Decode(raw string, target reflect.Type) (any, error) {
	if target == nil {
		return nil, fmt.Errorf("has no target Go type")
	}
	if target.Kind() == reflect.Pointer {
		value, err := Decode(raw, target.Elem())
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
	if !internalvaluekind.IsScalar(target.Kind()) {
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

// ValidateType reports whether target can be decoded as one HTTP scalar.
func ValidateType(target reflect.Type) error {
	if target == nil {
		return fmt.Errorf("HTTP-bound input has no Go type")
	}
	if internalvaluekind.IsScalar(target.Kind()) {
		return nil
	}
	return fmt.Errorf("contract input type %s does not support HTTP scalar decoding", target)
}
