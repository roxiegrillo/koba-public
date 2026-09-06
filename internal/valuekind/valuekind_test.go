package valuekind

import (
	"reflect"
	"testing"
)

func TestScalarAndNumericClassifications(t *testing.T) {
	for _, kind := range []reflect.Kind{
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
	} {
		if !IsNumeric(kind) || !IsScalar(kind) {
			t.Fatalf("numeric kind %s was not classified as numeric scalar", kind)
		}
	}
	for _, kind := range []reflect.Kind{reflect.String, reflect.Bool} {
		if IsNumeric(kind) || !IsScalar(kind) {
			t.Fatalf("scalar kind %s has wrong numeric/scalar classification", kind)
		}
	}
	for _, kind := range []reflect.Kind{reflect.Array, reflect.Slice, reflect.Map, reflect.Struct, reflect.Complex64} {
		if IsNumeric(kind) || IsScalar(kind) {
			t.Fatalf("structured kind %s was classified as scalar", kind)
		}
	}
}
