package koba

import (
	"reflect"
	"testing"
)

type methodValuesFixtureAPI struct{}

func (*methodValuesFixtureAPI) Endpoint() string {
	return "ok"
}

func (*methodValuesFixtureAPI) Routes() map[string]string {
	return map[string]string{"Endpoint": "/endpoint"}
}

func (*methodValuesFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{}
}

func TestGetMethodValueGenericTypesReturnsBoundBusinessMethods(t *testing.T) {
	methods := GetMethodValueGenericTypes(&methodValuesFixtureAPI{})
	if len(methods) != 1 {
		t.Fatalf("business method count = %d, want 1: %#v", len(methods), methods)
	}
	if got, want := methods[0].Left.Name, "Endpoint"; got != want {
		t.Fatalf("method name = %q, want %q", got, want)
	}
	values := methods[0].Right.Call(nil)
	if len(values) != 1 || values[0].String() != "ok" {
		t.Fatalf("bound method result = %#v, want ok", values)
	}
	if got, want := methods[0].Left.Type.Out(0), reflect.TypeOf(""); got != want {
		t.Fatalf("output type = %v, want %v", got, want)
	}
}
