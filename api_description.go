package koba

import (
	"net/http"
	"reflect"
)

// OperationDescription is the resolved operation model shared by execution
// plans and transport renderers. Route, HTTP method, request bindings, and
// Documentation describe the HTTP adapter around the typed API function.
type OperationDescription struct {
	MethodName  string
	OperationID string
	Route       string
	HTTPMethod  string
	InputType   reflect.Type
	OutputType  reflect.Type
	Contract    OperationContract
	// RequestBindings is the compiled transport-to-function construction plan
	// resolved from Contract.Request. It is stored separately from the authoring
	// descriptor so generated plans never need to serialize user-defined
	// companion structs.
	RequestBindings []RequestBinding
	Documentation   OperationDocumentation
}

// BodyBinding returns the binding which supplies the HTTP request body, when
// the operation has one. A request argument does not imply a body once a
// recursive request descriptor is present.
func (operation OperationDescription) BodyBinding() (RequestBinding, bool) {
	for _, binding := range operation.RequestBindings {
		if binding.Source == BindingSourceBody {
			return binding, true
		}
	}
	return RequestBinding{}, false
}

// ResolvedSuccessStatus returns the status emitted by the HTTP runtime and
// documented by OpenAPI. Keeping this rule on the resolved operation prevents
// reflected and generated applications from drifting.
func (operation OperationDescription) ResolvedSuccessStatus() int {
	if operation.Documentation.SuccessStatus != 0 {
		return operation.Documentation.SuccessStatus
	}
	if operation.OutputType == nil {
		return http.StatusNoContent
	}
	return http.StatusOK
}

// ResolvedRequiredScopes returns the operation's transport-neutral scope
// requirements. OperationDocumentation.RequiredScopes remains a compatibility
// fallback for APIs authored before authorization moved into APIContract.
// Validation rejects operations which declare scopes in both locations.
func (operation OperationDescription) ResolvedRequiredScopes() []string {
	if len(operation.Contract.Authorization.RequiredScopes) > 0 {
		return operation.Contract.Authorization.RequiredScopes
	}
	return operation.Documentation.RequiredScopes
}

// APIDescription contains the operations and optional metadata for one
// Koba API value.
type APIDescription struct {
	Name        string
	Description string
	Operations  []OperationDescription
}

// DescribeAPI resolves a Koba API without invoking any endpoint. A
// generated plan is used when available; otherwise the API is reflected. It
// is safe to pass a typed nil pointer as long as its metadata methods and
// generated plan do not dereference their receiver.
func DescribeAPI(api any) (APIDescription, error) {
	compiled, err := CompileAPI(api)
	if err != nil {
		return APIDescription{}, err
	}
	return compiled.Description, nil
}

// DescribeReflectedAPI explicitly selects the reflection description path.
// It is primarily used by generators and parity tests; normal application code
// should call DescribeAPI so generated plans can be selected transparently.
func DescribeReflectedAPI(api any) (APIDescription, error) {
	return describeReflectedAPI(api)
}
