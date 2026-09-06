package koba_test

import (
	"context"
	"errors"
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
	consume := operations["Consume"]
	if !consume.HasKafkaConsumerTransport() || consume.Route != "" || consume.HTTPMethod != "" {
		t.Fatalf("Consume transport = %#v, route = %q, method = %q", consume.Contract.Transport, consume.Route, consume.HTTPMethod)
	}
	if got, want := consume.Contract.Transport.KafkaConsumer.Topic, "static-fixture.events"; got != want {
		t.Fatalf("Consume topic = %q, want %q", got, want)
	}
	if encoding, encodingErr := consume.ResolvedKafkaMessageEncoding(); encodingErr != nil || encoding != fb.KafkaMessageEncodingJSON {
		t.Fatalf("Consume encoding = %q, error = %v", encoding, encodingErr)
	}
	connect := operations["Connect"]
	if got, want := connect.ResolvedSuccessStatus(), http.StatusSwitchingProtocols; got != want {
		t.Fatalf("Connect status = %d, want %d", got, want)
	}
	if connect.Contract.Transport.WebSocket == nil {
		t.Fatal("Connect transport has no WebSocket contract")
	}
	if got, want := operations["Update"].HTTPMethod, http.MethodPatch; got != want {
		t.Fatalf("Update method = %q, want %q", got, want)
	}
	create := operations["Create"]
	if create.Contract.Request != nil {
		t.Fatalf("Create generated contract retained raw request descriptor: %#v", create.Contract.Request)
	}
	if got, want := len(create.RequestBindings), 3; got != want {
		t.Fatalf("Create request binding count = %d, want %d", got, want)
	}
	createSubject := create.RequestBindings[0]
	if createSubject.Source != fb.BindingSourceSubject || createSubject.Name != "" || !reflect.DeepEqual(createSubject.FieldPath, []string{"Subject"}) || !reflect.DeepEqual(createSubject.FieldIndex, []int{0}) || createSubject.Input.GoType != reflect.TypeFor[string]() || !createSubject.Input.Required {
		t.Fatalf("Create subject binding = %#v, want required string Subject", createSubject)
	}
	createBody := create.RequestBindings[1]
	if createBody.Source != fb.BindingSourceBody || createBody.Name != "" || !reflect.DeepEqual(createBody.FieldPath, []string{"Payload"}) || !reflect.DeepEqual(createBody.FieldIndex, []int{1}) || createBody.Input.GoType != reflect.TypeFor[domain.Request]() || !createBody.Input.Required {
		t.Fatalf("Create body binding = %#v, want required Payload domain.Request body", createBody)
	}
	createName := create.RequestBindings[2]
	if createName.Source != fb.BindingSourceValue || !reflect.DeepEqual(createName.FieldPath, []string{"Payload", "Name"}) || !reflect.DeepEqual(createName.FieldIndex, []int{1, 0}) || !createName.Input.Required || createName.Input.MinLength == nil || *createName.Input.MinLength != 1 || createName.Input.MaxLength == nil || *createName.Input.MaxLength != 64 {
		t.Fatalf("Create Name binding = %#v, want constrained body value", createName)
	}
	reflectedDescription, err := fb.DescribeReflectedAPI((*staticfixture.API)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	var reflectedCreate fb.OperationDescription
	var reflectedCompose fb.OperationDescription
	var reflectedConnect fb.OperationDescription
	var reflectedConfigure fb.OperationDescription
	var reflectedConsume fb.OperationDescription
	for _, operation := range reflectedDescription.Operations {
		switch operation.MethodName {
		case "Create":
			reflectedCreate = operation
		case "Compose":
			reflectedCompose = operation
		case "Connect":
			reflectedConnect = operation
		case "Configure":
			reflectedConfigure = operation
		case "Consume":
			reflectedConsume = operation
		}
	}
	if !reflect.DeepEqual(consume.Contract.Transport, reflectedConsume.Contract.Transport) ||
		consume.InputType != reflectedConsume.InputType {
		t.Fatalf("Consume differs between active and reflected plans:\nactive:    %#v\nreflected: %#v", consume, reflectedConsume)
	}
	if !reflect.DeepEqual(create.RequestBindings, reflectedCreate.RequestBindings) {
		t.Fatalf("Create request bindings differ between active and reflected plans:\nactive:    %#v\nreflected: %#v", create.RequestBindings, reflectedCreate.RequestBindings)
	}
	compose := operations["Compose"]
	if got, want := compose.Route, "/fixtures/:id/compose"; got != want {
		t.Fatalf("Compose route = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(compose.RequestBindings, reflectedCompose.RequestBindings) {
		t.Fatalf("Compose request bindings differ between active and reflected plans:\nactive:    %#v\nreflected: %#v", compose.RequestBindings, reflectedCompose.RequestBindings)
	}
	if !reflect.DeepEqual(compose.ResponseBindings, reflectedCompose.ResponseBindings) {
		t.Fatalf("Compose response bindings differ between active and reflected plans:\nactive:    %#v\nreflected: %#v", compose.ResponseBindings, reflectedCompose.ResponseBindings)
	}
	if got, want := len(compose.ResponseBindings), 1; got != want {
		t.Fatalf("Compose response binding count = %d, want %d", got, want)
	}
	composeResponseID := compose.ResponseBindings[0]
	if !reflect.DeepEqual(composeResponseID.FieldPath, []string{"ID"}) ||
		!composeResponseID.Input.Required || composeResponseID.Input.MinLength == nil || *composeResponseID.Input.MinLength != 1 ||
		composeResponseID.Documentation == nil || composeResponseID.Documentation.Description != "Composed fixture identifier." {
		t.Fatalf("Compose response ID binding = %#v", composeResponseID)
	}
	composeBody, ok := compose.BodyBinding()
	if !ok || len(composeBody.ObjectRules) != 2 ||
		composeBody.ObjectRules[0].Kind() != fb.ObjectRuleAllOrNone ||
		composeBody.ObjectRules[1].Kind() != fb.ObjectRuleSameNullState ||
		!reflect.DeepEqual(composeBody.ObjectRules[0].Fields(), []string{"Latitude", "Longitude"}) ||
		!reflect.DeepEqual(composeBody.ObjectRules[1].Fields(), []string{"Latitude", "Longitude"}) {
		t.Fatalf("Compose body object rules = %#v, want paired presence and null-state rules", composeBody.ObjectRules)
	}
	if got := composeBody.Documentation; got == nil || got.Description != "Composition payload." {
		t.Fatalf("Compose body documentation = %#v", got)
	}
	if got, want := composeBody.Documentation.Example, (domain.ComposePayload{
		Name:    "fixture",
		Details: domain.ComposeDetails{Note: "nested fixture"},
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Compose body example = %#v, want %#v", got, want)
	}
	if got := compose.RequestBindings[3].Documentation; got == nil || got.Description != "Composition name." {
		t.Fatalf("Compose name documentation = %#v", got)
	}
	replaceBody, ok := operations["Replace"].BodyBinding()
	if !ok || replaceBody.Input.MinItems == nil || *replaceBody.Input.MinItems != 1 ||
		replaceBody.Input.MaxItems == nil || *replaceBody.Input.MaxItems != 3 ||
		!replaceBody.Input.UniqueItems || replaceBody.Input.Items == nil ||
		replaceBody.Input.Items.Input.GoType != reflect.TypeFor[string]() ||
		replaceBody.Input.Items.Documentation == nil ||
		replaceBody.Input.Items.Documentation.Description != "Fixture identifier." {
		t.Fatalf("Replace body collection contract = %#v, want 1..3 items", replaceBody)
	}
	if !reflect.DeepEqual(connect.Contract.Transport, reflectedConnect.Contract.Transport) {
		t.Fatalf("Connect transport differs between active and reflected plans:\nactive:    %#v\nreflected: %#v", connect.Contract.Transport, reflectedConnect.Contract.Transport)
	}
	configure := operations["Configure"]
	if !reflect.DeepEqual(configure.RequestBindings, reflectedConfigure.RequestBindings) {
		t.Fatalf("Configure request bindings differ between active and reflected plans:\nactive:    %#v\nreflected: %#v", configure.RequestBindings, reflectedConfigure.RequestBindings)
	}
	if got, want := len(configure.RequestBindings), 3; got != want {
		t.Fatalf("Configure request binding count = %d, want %d", got, want)
	}
	attributes := configure.RequestBindings[1].Input
	if !attributes.HasDefault || !reflect.DeepEqual(attributes.Default, map[string]any{
		"presentation": map[string]any{"labels": []any{}},
	}) {
		t.Fatalf("Configure attributes default = %#v", attributes.Default)
	}
	tags := configure.RequestBindings[2].Input
	if !tags.HasDefault || tags.Default == nil || !tags.UniqueItems || tags.Items == nil ||
		len(tags.Items.Bindings) != 3 || tags.Items.Descriptor != nil ||
		!reflect.DeepEqual(tags.Items.Bindings[2].FieldPath, []string{"Details", "Code"}) {
		t.Fatalf("Configure tags contract = %#v", tags)
	}
	socket := connect.Contract.Transport.WebSocket.Socket
	if socket.ClientToServer == nil || socket.ClientToServer.Descriptor != nil ||
		len(socket.ClientToServer.Bindings) != 1 || socket.ServerToClient == nil ||
		len(socket.ServerToClient.Bindings) != 2 ||
		socket.ServerToClient.Bindings[0].Input.Format != "fixture-id" {
		t.Fatalf("Connect directional message contracts = %#v", socket)
	}
	if got := compose.RequestBindings[0].Documentation; got == nil || got.Description != "Fixture ID" {
		t.Fatalf("Compose path documentation = %#v, want Fixture ID", got)
	}
	if got := compose.RequestBindings[1].Documentation; got == nil || got.Description != "Optional page size" {
		t.Fatalf("Compose query documentation = %#v, want Optional page size", got)
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
		Input: domain.CreateRequest{
			Subject: "fixture-user",
			Payload: domain.Request{Name: "fixture-1"},
		},
		HasInput: true,
	})
	if err != nil {
		t.Fatalf("Create invoker returned an error: %v", err)
	}
	if response, ok := created.Value.(domain.Response); !ok || response.ID != "fixture-1" || !created.HasValue {
		t.Fatalf("Create result = %#v, want domain.Response", created)
	}
	if api.PrincipalID != "fixture-user" {
		t.Fatalf("Create subject = %q, want fixture-user", api.PrincipalID)
	}
	wantCreateError := errors.New("create fixture failed")
	api.CreateError = wantCreateError
	failedCreate, err := compiled.Invokers["Create"](fb.Invocation{
		Context: context.Background(),
		Input: domain.CreateRequest{
			Subject: "fixture-user",
			Payload: domain.Request{Name: "fixture-2"},
		},
		HasInput: true,
	})
	if !errors.Is(err, wantCreateError) || failedCreate.HasValue || failedCreate.Value != nil {
		t.Fatalf("Create failure = %#v, error = %v; want discarded response and %v", failedCreate, err, wantCreateError)
	}
	api.CreateError = nil
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
	wantDeleteError := errors.New("delete fixture failed")
	api.DeleteError = wantDeleteError
	failedDelete, err := compiled.Invokers["Delete"](fb.Invocation{Context: context.Background()})
	if !errors.Is(err, wantDeleteError) || failedDelete.HasValue || failedDelete.Value != nil {
		t.Fatalf("Delete failure = %#v, error = %v; want void result and %v", failedDelete, err, wantDeleteError)
	}

	consumed, err := compiled.Invokers["Consume"](fb.Invocation{
		Context:  context.Background(),
		Input:    domain.KafkaEvent{ID: "event-1"},
		HasInput: true,
	})
	if err != nil || consumed.HasValue || api.ConsumedEvent.ID != "event-1" {
		t.Fatalf("Consume result = %#v, error = %v, event = %#v", consumed, err, api.ConsumedEvent)
	}
	wantConsumeError := errors.New("retry fixture event")
	api.ConsumeError = wantConsumeError
	_, err = compiled.Invokers["Consume"](fb.Invocation{
		Context:  context.Background(),
		Input:    domain.KafkaEvent{ID: "event-2"},
		HasInput: true,
	})
	if !errors.Is(err, wantConsumeError) {
		t.Fatalf("Consume error = %v, want %v", err, wantConsumeError)
	}
}

func TestStaticFixtureContractlessOperationPreservesNilInputs(t *testing.T) {
	active, err := fb.DescribeAPI((*staticfixture.API)(nil))
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	reflected, err := fb.DescribeReflectedAPI((*staticfixture.API)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}

	findDelete := func(description fb.APIDescription) fb.OperationDescription {
		for _, operation := range description.Operations {
			if operation.MethodName == "Delete" {
				return operation
			}
		}
		t.Fatal("Delete operation is missing")
		return fb.OperationDescription{}
	}
	activeDelete := findDelete(active)
	reflectedDelete := findDelete(reflected)
	if reflectedDelete.Contract.Inputs != nil {
		t.Fatalf("reflected Delete inputs = %#v, want nil", reflectedDelete.Contract.Inputs)
	}
	if activeDelete.Contract.Inputs != nil {
		t.Fatalf("active Delete inputs = %#v, want nil", activeDelete.Contract.Inputs)
	}
}
