package koba_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
)

func TestStaticFixtureExecutionPlan(t *testing.T) {
	api := &staticfixture.API{}
	_, hasStaticProvider := any(api).(fb.WithCompiledAPI)
	if hasStaticProvider != staticProviderExpected {
		t.Fatalf("static provider present = %v, want %v", hasStaticProvider, staticProviderExpected)
	}

	compiled, err := fb.CompileAPI(api)
	if err != nil {
		t.Fatalf("CompileAPI() returned an error: %v", err)
	}
	operations := make(map[string]fb.OperationDescription, len(compiled.Description.Operations))
	for _, operation := range compiled.Description.Operations {
		operations[operation.MethodName] = operation
	}
	if got, want := operations["Create"].ResolvedSuccessStatus(), http.StatusCreated; got != want {
		t.Fatalf("Create status = %d, want %d", got, want)
	}
	if got, want := operations["Delete"].HTTPMethod, http.MethodDelete; got != want {
		t.Fatalf("Delete method = %q, want %q", got, want)
	}
	if got, want := operations["Delete"].ResolvedSuccessStatus(), http.StatusNoContent; got != want {
		t.Fatalf("Delete status = %d, want %d", got, want)
	}
	if got, want := operations["Replace"].HTTPMethod, http.MethodPut; got != want {
		t.Fatalf("Replace method = %q, want %q", got, want)
	}
	if got, want := operations["Update"].HTTPMethod, http.MethodPatch; got != want {
		t.Fatalf("Update method = %q, want %q", got, want)
	}
	create := operations["Create"]
	if create.Contract.Request != nil {
		t.Fatalf("Create generated contract retained raw request descriptor: %#v", create.Contract.Request)
	}
	if got, want := len(create.RequestBindings), 2; got != want {
		t.Fatalf("Create request binding count = %d, want %d", got, want)
	}
	createBody := create.RequestBindings[0]
	if createBody.Source != fb.BindingSourceBody || createBody.Name != "" || len(createBody.FieldPath) != 0 || len(createBody.FieldIndex) != 0 || createBody.Input.GoType != reflect.TypeFor[domain.Request]() || !createBody.Input.Required {
		t.Fatalf("Create body binding = %#v, want required root domain.Request body", createBody)
	}
	createName := create.RequestBindings[1]
	if createName.Source != fb.BindingSourceValue || !reflect.DeepEqual(createName.FieldPath, []string{"Name"}) || !reflect.DeepEqual(createName.FieldIndex, []int{0}) || !createName.Input.Required || createName.Input.MinLength == nil || *createName.Input.MinLength != 1 || createName.Input.MaxLength == nil || *createName.Input.MaxLength != 64 {
		t.Fatalf("Create Name binding = %#v, want constrained body value", createName)
	}
	reflectedDescription, err := fb.DescribeReflectedAPI((*staticfixture.API)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	var reflectedCreate fb.OperationDescription
	for _, operation := range reflectedDescription.Operations {
		if operation.MethodName == "Create" {
			reflectedCreate = operation
			break
		}
	}
	if !reflect.DeepEqual(create.RequestBindings, reflectedCreate.RequestBindings) {
		t.Fatalf("Create request bindings differ between active and reflected plans:\nactive:    %#v\nreflected: %#v", create.RequestBindings, reflectedCreate.RequestBindings)
	}
	if got := create.Contract.Authorization.RequiredScopes; len(got) != 1 || got[0] != "create:fixtures" {
		t.Fatalf("Create contract scopes = %v, want [create:fixtures]", got)
	}
	if len(create.Documentation.RequiredScopes) != 0 {
		t.Fatalf("Create scopes unexpectedly came from HTTP documentation: %v", create.Documentation.RequiredScopes)
	}
	inspect := operations["Inspect"]
	if got, want := len(inspect.Contract.Inputs), 4; got != want {
		t.Fatalf("Inspect contract input count = %d, want %d", got, want)
	}
	if pageSize := inspect.Contract.Inputs["pageSize"]; !pageSize.HasDefault || pageSize.Default != domain.PageSize(20) || pageSize.Minimum != domain.PageSize(1) || pageSize.Maximum != domain.PageSize(100) {
		t.Fatalf("Inspect pageSize contract = %#v", pageSize)
	}

	created, err := compiled.Invokers["Create"](fb.Invocation{
		Context: context.Background(),
		DecodeBody: func(target any) error {
			request := target.(*domain.Request)
			request.Name = "fixture-1"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Create invoker returned an error: %v", err)
	}
	if response, ok := created.Value.(domain.Response); !ok || response.ID != "fixture-1" || !created.HasValue {
		t.Fatalf("Create result = %#v, want domain.Response", created)
	}
	inputs, err := fb.ValidateOperationInputs(inspect.Contract, map[string]any{
		"fixtureID": "fixture-1",
		"traceID":   "trace-1",
	})
	if err != nil {
		t.Fatalf("ValidateOperationInputs() returned an error: %v", err)
	}
	inspected, err := compiled.Invokers["Inspect"](fb.Invocation{
		Context: fb.ContextWithOperationInputs(context.Background(), inputs),
	})
	if err != nil {
		t.Fatalf("Inspect invoker returned an error: %v", err)
	}
	if response, ok := inspected.Value.(domain.Response); !ok || response.ID != "fixture-1" || api.PageSize != 20 {
		t.Fatalf("Inspect result = %#v, page size = %d", inspected, api.PageSize)
	}

	nilResult, err := compiled.Invokers["Nil"](fb.Invocation{Context: context.Background()})
	if err != nil || !nilResult.HasValue || nilResult.Value != nil {
		t.Fatalf("Nil result = %#v, error = %v; want an intentional nil value", nilResult, err)
	}
	deleted, err := compiled.Invokers["Delete"](fb.Invocation{Context: context.Background()})
	if err != nil || deleted.HasValue {
		t.Fatalf("Delete result = %#v, error = %v; want void result", deleted, err)
	}
}
