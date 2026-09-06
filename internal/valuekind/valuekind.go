// Package valuekind classifies Go value kinds shared by contracts and
// transport adapters.
package valuekind

import "reflect"

// IsNumeric reports whether kind is a supported real numeric value.
func IsNumeric(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// IsScalar reports whether kind can be represented as one contract scalar.
func IsScalar(kind reflect.Kind) bool {
	return kind == reflect.String || kind == reflect.Bool || IsNumeric(kind)
}
