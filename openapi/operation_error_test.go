package openapi

import (
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

type nativeErrorResponse struct {
	Message string `json:"message"`
}

type nativeErrorAPI struct{}

func (*nativeErrorAPI) ErrorOnly() error { return nil }

func (*nativeErrorAPI) Value() (nativeErrorResponse, error) {
	return nativeErrorResponse{Message: "ok"}, nil
}

func (*nativeErrorAPI) Routes() map[string]string {
	return map[string]string{
		"ErrorOnly": "/native-errors/void",
		"Value":     "/native-errors/value",
	}
}

func (*nativeErrorAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"ErrorOnly": {OperationID: "runNativeErrorOnly"},
		"Value":     {OperationID: "getNativeErrorValue"},
	}}
}

func TestBuildProjectsNativeErrorResultsAsSuccessValues(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*nativeErrorAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Native errors", Version: "1.0.0"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}

	voidOperation := document.Paths.Find("/native-errors/void").Get
	voidResponse := voidOperation.Responses.Value("204")
	if voidResponse == nil || voidResponse.Value == nil {
		t.Fatal("error-only operation has no 204 success response")
	}
	if len(voidResponse.Value.Content) != 0 {
		t.Fatalf("error-only success content = %#v, want no response schema", voidResponse.Value.Content)
	}

	valueOperation := document.Paths.Find("/native-errors/value").Get
	valueResponse := valueOperation.Responses.Value("200")
	if valueResponse == nil || valueResponse.Value == nil {
		t.Fatal("response-and-error operation has no 200 success response")
	}
	media := valueResponse.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatal("response-and-error operation has no response value schema")
	}
	if media.Schema.Value.Properties["message"] == nil {
		t.Fatalf("response schema properties = %#v, want message", media.Schema.Value.Properties)
	}
}
