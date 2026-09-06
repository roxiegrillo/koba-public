package httpscalar

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type structuredTextScalar [16]byte

func (*structuredTextScalar) UnmarshalText([]byte) error { return nil }

type namedTextScalar string

func (value *namedTextScalar) UnmarshalText(text []byte) error {
	if string(text) != "valid" {
		return fmt.Errorf("want valid")
	}
	*value = "decoded"
	return nil
}

func TestStructuredTextUnmarshalersRemainUnsupported(t *testing.T) {
	if err := ValidateType(reflect.TypeFor[structuredTextScalar]()); err == nil || !strings.Contains(err.Error(), "does not support HTTP scalar decoding") {
		t.Fatalf("ValidateType(structuredTextScalar) error = %v, want unsupported scalar", err)
	}
	if _, err := Decode("anything", reflect.TypeFor[structuredTextScalar]()); err == nil || !strings.Contains(err.Error(), "does not support HTTP scalar decoding") {
		t.Fatalf("Decode(structuredTextScalar) error = %v, want unsupported scalar", err)
	}
}

func TestNamedPrimitiveTextUnmarshalersRemainSupported(t *testing.T) {
	if err := ValidateType(reflect.TypeFor[namedTextScalar]()); err != nil {
		t.Fatalf("ValidateType(namedTextScalar) returned an error: %v", err)
	}
	value, err := Decode("valid", reflect.TypeFor[namedTextScalar]())
	if err != nil {
		t.Fatalf("Decode(namedTextScalar) returned an error: %v", err)
	}
	if got, want := value.(namedTextScalar), namedTextScalar("decoded"); got != want {
		t.Fatalf("decoded value = %q, want %q", got, want)
	}
	if _, err := Decode("invalid", reflect.TypeFor[namedTextScalar]()); err == nil || !strings.Contains(err.Error(), "want valid") {
		t.Fatalf("invalid named scalar error = %v, want unmarshaler error", err)
	}
}

func TestDecodePrimitiveScalars(t *testing.T) {
	for _, test := range []struct {
		name   string
		raw    string
		target reflect.Type
		want   any
	}{
		{name: "string", raw: "value", target: reflect.TypeFor[string](), want: "value"},
		{name: "boolean", raw: "true", target: reflect.TypeFor[bool](), want: true},
		{name: "integer", raw: "42", target: reflect.TypeFor[int](), want: 42},
		{name: "unsigned", raw: "42", target: reflect.TypeFor[uint](), want: uint(42)},
		{name: "float", raw: "4.2", target: reflect.TypeFor[float64](), want: 4.2},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Decode(test.raw, test.target)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Decode(%q, %s) = %#v, %v; want %#v", test.raw, test.target, got, err, test.want)
			}
		})
	}
}

func TestDecodePointerAndInvalidWireValues(t *testing.T) {
	value, err := Decode("42", reflect.TypeFor[*int]())
	if err != nil || value.(*int) == nil || *value.(*int) != 42 {
		t.Fatalf("Decode(pointer) = %#v, %v", value, err)
	}

	for _, test := range []struct {
		name   string
		raw    string
		target reflect.Type
	}{
		{name: "invalid boolean", raw: "perhaps", target: reflect.TypeFor[bool]()},
		{name: "integer overflow", raw: "128", target: reflect.TypeFor[int8]()},
		{name: "not a number", raw: "NaN", target: reflect.TypeFor[float64]()},
		{name: "infinity", raw: "+Inf", target: reflect.TypeFor[float64]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(test.raw, test.target); err == nil {
				t.Fatalf("Decode(%q, %s) returned nil error", test.raw, test.target)
			}
		})
	}

	if err := ValidateType(nil); err == nil {
		t.Fatal("ValidateType(nil) returned nil error")
	}
}
