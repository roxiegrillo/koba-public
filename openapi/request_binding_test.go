package openapi

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

type projectedRequestBindingsInput struct {
	Filters struct {
		PageSize int `json:"page_size"`
	} `json:"filters"`
	Identity struct {
		ID      string `json:"id"`
		TraceID string `json:"trace_id"`
		Session string `json:"session"`
	} `json:"identity"`
}

type projectedRequestBody struct {
	Name    string                     `json:"name"`
	Details projectedRequestBodyDetail `json:"details"`
}

type projectedRequestBodyDetail struct {
	Note string `json:"note"`
}

type projectedMixedRequest struct {
	ID      string               `json:"id"`
	Payload projectedRequestBody `json:"payload"`
}

func TestBuildProjectsNestedRequestBindingsAsParametersWithoutRequestBody(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listThings",
		Route:       "/things/:id",
		HTTPMethod:  http.MethodGet,
		InputType:   reflect.TypeFor[projectedRequestBindingsInput](),
		RequestBindings: []fb.RequestBinding{
			{
				Source:     fb.BindingSourceQuery,
				Name:       "page_size",
				FieldPath:  []string{"Filters", "PageSize"},
				FieldIndex: []int{0, 0},
				Input:      fb.InputOf[int](fb.Default(20), fb.Min(1), fb.Max(100)),
			},
			{
				Source:     fb.BindingSourcePath,
				Name:       "id",
				FieldPath:  []string{"Identity", "ID"},
				FieldIndex: []int{1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1)),
			},
			{
				Source:     fb.BindingSourceHeader,
				Name:       "X-Trace-ID",
				FieldPath:  []string{"Identity", "TraceID"},
				FieldIndex: []int{1, 1},
				Input:      fb.InputOf[string](fb.Required(), fb.WithFormat("uuid")),
			},
			{
				Source:     fb.BindingSourceCookie,
				Name:       "session",
				FieldPath:  []string{"Identity", "Session"},
				FieldIndex: []int{1, 2},
				Input:      fb.InputOf[string](fb.MaxLength(64)),
			},
		},
		Documentation: fb.OperationDocumentation{Parameters: []fb.ParameterDocumentation{
			{
				Name:        "page_size",
				In:          fb.ParameterInQuery,
				Description: "Maximum number of things to return",
				Deprecated:  true,
				Example:     42,
			},
			{
				Name:        "x-trace-id",
				In:          fb.ParameterInHeader,
				Description: "Request correlation ID",
				Example:     "33197c6a-c56a-4567-a850-aab93c0d6c61",
			},
		}},
	}}}

	document, err := Build(Config{Title: "Things", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/things/{id}").Get
	if operation.RequestBody != nil {
		t.Fatalf("query-only request generated a request body: %#v", operation.RequestBody)
	}

	parameters := make(map[string]*openapi3.Parameter, len(operation.Parameters))
	for _, reference := range operation.Parameters {
		parameters[reference.Value.Name] = reference.Value
	}
	if len(parameters) != 4 {
		t.Fatalf("parameters = %#v, want four request bindings", parameters)
	}
	pageSize := parameters["page_size"]
	if pageSize == nil || pageSize.In != openapi3.ParameterInQuery {
		t.Fatalf("page_size parameter = %#v", pageSize)
	}
	if pageSize.Description != "Maximum number of things to return" || !pageSize.Deprecated || pageSize.Example != 42 {
		t.Errorf("page_size documentation = %#v", pageSize)
	}
	if pageSize.Required {
		t.Error("defaulted page_size binding was rendered as required")
	}
	if schema := pageSize.Schema.Value; schema.Default != 20 || schema.Min == nil || *schema.Min != 1 || schema.Max == nil || *schema.Max != 100 {
		t.Errorf("page_size schema = %#v", schema)
	}
	assertParameter(t, parameters["id"], openapi3.ParameterInPath, true)
	assertParameter(t, parameters["X-Trace-ID"], openapi3.ParameterInHeader, true)
	assertParameter(t, parameters["session"], openapi3.ParameterInCookie, false)
	traceID := parameters["X-Trace-ID"]
	if traceID.Description != "Request correlation ID" || traceID.Example != "33197c6a-c56a-4567-a850-aab93c0d6c61" {
		t.Errorf("header documentation was not matched case-insensitively: %#v", traceID)
	}
	if traceID.Schema.Value.Format != "uuid" {
		t.Errorf("trace ID format = %q, want uuid", traceID.Schema.Value.Format)
	}
}

func TestBuildUsesBodyBindingTargetTypeForMixedRequest(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Update",
		OperationID: "updateThing",
		Route:       "/things/:id",
		HTTPMethod:  http.MethodPatch,
		InputType:   reflect.TypeFor[projectedMixedRequest](),
		RequestBindings: []fb.RequestBinding{
			{
				Source:     fb.BindingSourcePath,
				Name:       "id",
				FieldPath:  []string{"ID"},
				FieldIndex: []int{0},
				Input:      fb.InputOf[string](fb.Required()),
			},
			{
				Source:     fb.BindingSourceBody,
				FieldPath:  []string{"Payload"},
				FieldIndex: []int{1},
				Input:      fb.InputOf[projectedRequestBody](fb.Required()),
			},
			{
				Source:     fb.BindingSourceValue,
				FieldPath:  []string{"Payload", "Name"},
				FieldIndex: []int{1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(3), fb.MaxLength(40), fb.Matches(`^[A-Z]`)),
			},
			{
				Source:     fb.BindingSourceValue,
				FieldPath:  []string{"Payload", "Details", "Note"},
				FieldIndex: []int{1, 1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1)),
			},
		},
		Documentation: fb.OperationDocumentation{
			RequestBodyDescription: "Fields to update",
		},
	}}}

	document, err := Build(Config{Title: "Things", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/things/{id}").Patch
	if len(operation.Parameters) != 1 || operation.Parameters[0].Value.Name != "id" {
		t.Fatalf("parameters = %#v, want only the path binding", operation.Parameters)
	}
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		t.Fatal("body binding did not generate a request body")
	}
	if !operation.RequestBody.Value.Required {
		t.Error("required body binding generated an optional request body")
	}
	if operation.RequestBody.Value.Description != "Fields to update" {
		t.Errorf("request body description = %q", operation.RequestBody.Value.Description)
	}
	media := operation.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("request body media = %#v", media)
	}
	if wrongRef := "#/components/schemas/" + componentName(reflect.TypeFor[projectedMixedRequest]()); media.Schema.Ref == wrongRef {
		t.Errorf("request body schema used complete operation input type %q", wrongRef)
	}
	bodySchema := media.Schema.Value
	if bodySchema == nil {
		t.Fatalf("request body schema = %#v, want request-local schema", media.Schema)
	}
	if !containsString(bodySchema.Required, "name") || !containsString(bodySchema.Required, "details") {
		t.Errorf("body required fields = %v, want name and details", bodySchema.Required)
	}
	name := bodySchema.Properties["name"]
	if name == nil || name.Value == nil || name.Value.MinLength != 3 || name.Value.MaxLength == nil || *name.Value.MaxLength != 40 || name.Value.Pattern != `^[A-Z]` {
		t.Errorf("body name contract = %#v", name)
	}
	details := resolvedSchema(bodySchema.Properties["details"], document.Components.Schemas)
	if details == nil || !containsString(details.Required, "note") {
		t.Fatalf("nested body contract = %#v, want required note", details)
	}
	note := details.Properties["note"]
	if note == nil || note.Value == nil || note.Value.MinLength != 1 {
		t.Errorf("nested note contract = %#v", note)
	}
}

func TestBuildProjectsRootBodyConstraints(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Echo",
		OperationID: "echo",
		Route:       "/echo",
		HTTPMethod:  http.MethodPost,
		InputType:   reflect.TypeFor[string](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input: fb.InputOf[string](
				fb.Default("hello"),
				fb.MinLength(3),
				fb.MaxLength(20),
				fb.Matches(`^[a-z]+$`),
			),
		}},
	}}}

	document, err := Build(Config{Title: "Echo", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/echo").Post.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("body schema = %#v", media)
	}
	schema := media.Schema.Value
	if schema.Default != "hello" || schema.MinLength != 3 || schema.MaxLength == nil || *schema.MaxLength != 20 || schema.Pattern != `^[a-z]+$` {
		t.Errorf("body contract schema = %#v", schema)
	}
}
