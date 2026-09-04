package koba

import (
	"reflect"
	"strings"
	"testing"
)

func TestUnBindErrorReturnsValues(t *testing.T) {
	want := reflect.ValueOf("ok")
	values, failure := UnBindError(func() []reflect.Value {
		return []reflect.Value{want}
	})

	if failure.Error != nil {
		t.Fatalf("unexpected recovered error: %v", failure.Error)
	}
	if len(values) != 1 || values[0].String() != want.String() {
		t.Fatalf("values = %#v, want %q", values, want.String())
	}
}

func TestUnBindErrorRecoversPanicWithStack(t *testing.T) {
	values, failure := UnBindError(func() []reflect.Value {
		panic("boom")
	})

	if values != nil {
		t.Fatalf("values = %#v, want nil", values)
	}
	if got, want := failure.Error, "boom"; got != want {
		t.Fatalf("recovered error = %#v, want %q", got, want)
	}
	if !strings.Contains(failure.Stack, "boom") {
		t.Fatalf("stack does not contain recovered panic: %q", failure.Stack)
	}
}
