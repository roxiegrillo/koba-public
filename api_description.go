package koba

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
)

// ParameterLocation identifies where an operation reads a parameter which is
// not represented by its Go request-body argument.
type ParameterLocation string

const (
	ParameterInQuery  ParameterLocation = "query"
	ParameterInHeader ParameterLocation = "header"
	ParameterInCookie ParameterLocation = "cookie"
	ParameterInPath   ParameterLocation = "path"
)

// ParameterDocumentation describes an input read from context (query,
// header, cookie, or path). GoType is used to derive its OpenAPI schema.
//
// Koba can infer a JSON request body from a function argument,
// but context values are intentionally transport-agnostic and therefore need
// this small amount of explicit metadata.
type ParameterDocumentation struct {
	Name        string
	In          ParameterLocation
	Description string
	Required    bool
	Deprecated  bool
	GoType      reflect.Type
	Format      string
	Pattern     string
	Enum        []any
	Example     any
}

// ParameterOf creates typed parameter metadata without requiring callers to
// import reflect.
func ParameterOf[T any](name string, in ParameterLocation, description string, required bool) ParameterDocumentation {
	return ParameterDocumentation{
		Name:        name,
		In:          in,
		Description: description,
		Required:    required,
		GoType:      reflect.TypeFor[T](),
	}
}

// ResponseDocumentation documents a response which cannot be inferred from
// the function's success return type, normally a domain error response.
type ResponseDocumentation struct {
	Description string
}

// SecurityRequirement is one authentication alternative. Every named scheme
// in one requirement is required together (OpenAPI AND semantics). A slice of
// requirements represents alternatives (OpenAPI OR semantics).
type SecurityRequirement []string

// OperationDocumentation contains transport-neutral metadata keyed by Go
// method name in APIDocumentation. Method and SuccessStatus may override the
// defaults inferred from the method signature; the remaining fields enrich
// generated contracts.
type OperationDocumentation struct {
	// Method overrides the HTTP method inferred from the Go signature.
	// Supported values are GET, POST, PUT, PATCH, and DELETE.
	Method string
	// SuccessStatus overrides the inferred success status. Operations which
	// return a value default to 200; void operations default to 204.
	SuccessStatus          int
	OperationID            string
	Summary                string
	Description            string
	Tags                   []string
	Parameters             []ParameterDocumentation
	RequestBodyDescription string
	Responses              map[int]ResponseDocumentation
	Deprecated             bool
	// Security overrides the document-level security requirements when it is
	// non-nil. Items inside one requirement are combined with AND; multiple
	// requirements are alternatives combined with OR.
	Security []SecurityRequirement
	// Public explicitly disables document-level security for this operation.
	Public bool
}

// APIDocumentation is optional, transport-neutral documentation supplied by an
// API value. Operations is keyed by exported Go method name.
type APIDocumentation struct {
	Name        string
	Description string
	Operations  map[string]OperationDocumentation
}

// WithAPIDocumentation is implemented by API values that want richer generated
// documentation than reflection alone can provide. Implementations should be
// metadata-only so the method can safely be called on a typed nil receiver by
// build-time generators.
type WithAPIDocumentation interface {
	APIDocumentation() APIDocumentation
}

// OperationDescription is the neutral contract used both by execution plans
// and by documentation renderers.
type OperationDescription struct {
	MethodName    string
	OperationID   string
	Route         string
	HTTPMethod    string
	InputType     reflect.Type
	OutputType    reflect.Type
	Documentation OperationDocumentation
}

// ResolvedSuccessStatus returns the status emitted by the runtime and
// documented by OpenAPI. Keeping this rule on the neutral operation contract
// prevents reflected and generated applications from drifting.
func (operation OperationDescription) ResolvedSuccessStatus() int {
	if operation.Documentation.SuccessStatus != 0 {
		return operation.Documentation.SuccessStatus
	}
	if operation.OutputType == nil {
		return http.StatusNoContent
	}
	return http.StatusOK
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

func describeReflectedAPI(api any) (description APIDescription, err error) {
	if api == nil || reflect.TypeOf(api) == nil {
		return APIDescription{}, fmt.Errorf("koba: API value is nil; pass an API value or typed nil pointer")
	}

	documentation, err := apiDocumentation(api)
	if err != nil {
		return APIDescription{}, err
	}
	description.Name = documentation.Name
	description.Description = documentation.Description

	routes, err := apiRoutes(api)
	if err != nil {
		return APIDescription{}, err
	}

	typeOfAPI := reflect.TypeOf(api)
	for index := 0; index < typeOfAPI.NumMethod(); index++ {
		method := typeOfAPI.Method(index)
		if !method.IsExported() || isNotExportable(method) {
			continue
		}
		inputTypes, outputTypes := GetInputOutputTypes(method)
		post, methodErr := isPost(inputTypes, outputTypes)
		if methodErr != nil {
			return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, methodErr)
		}

		httpMethod := http.MethodGet
		if post {
			httpMethod = http.MethodPost
		}
		route, routeIsExplicit := routes[method.Name]
		if !routeIsExplicit {
			route = "/" + method.Name
		}
		operationDocumentation := documentation.Operations[method.Name]
		if explicitMethod := strings.ToUpper(strings.TrimSpace(operationDocumentation.Method)); explicitMethod != "" {
			switch explicitMethod {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				httpMethod = explicitMethod
			default:
				return APIDescription{}, fmt.Errorf("koba: describe %s: unsupported HTTP method %q", method.Name, operationDocumentation.Method)
			}
		}
		operationID := strings.TrimSpace(operationDocumentation.OperationID)
		if operationID == "" {
			operationID = defaultOperationID(typeOfAPI, method.Name)
		}
		operation := OperationDescription{
			MethodName:    method.Name,
			OperationID:   operationID,
			Route:         route,
			HTTPMethod:    httpMethod,
			Documentation: operationDocumentation,
		}
		if bodyType := bodyInputType(inputTypes); bodyType != nil {
			operation.InputType = bodyType
		}
		if len(outputTypes) == 1 {
			operation.OutputType = outputTypes[0]
		}
		description.Operations = append(description.Operations, operation)
	}

	if err := validateDocumentationCoverage(documentation, description.Operations); err != nil {
		return APIDescription{}, err
	}
	return description, nil
}

func defaultOperationID(apiType reflect.Type, methodName string) string {
	for apiType.Kind() == reflect.Pointer {
		apiType = apiType.Elem()
	}
	prefix := apiType.PkgPath() + "/" + apiType.Name()
	prefix = identifierPrefix(prefix)
	if prefix == "" {
		prefix = "api"
	}
	return prefix + methodName
}

func identifierPrefix(value string) string {
	var result strings.Builder
	upperNext := false
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			upperNext = result.Len() > 0
			continue
		}
		if result.Len() == 0 {
			character = unicode.ToLower(character)
		} else if upperNext {
			character = unicode.ToUpper(character)
		}
		result.WriteRune(character)
		upperNext = false
	}
	return result.String()
}

func apiRoutes(api any) (routes map[string]string, err error) {
	routes = map[string]string{}
	withRoutes, ok := api.(WithRoutesAPI)
	if !ok {
		return routes, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("koba: Routes panicked while describing API: %v", recovered)
		}
	}()
	for method, route := range withRoutes.Routes() {
		routes[method] = route
	}
	return routes, nil
}

func apiDocumentation(api any) (documentation APIDocumentation, err error) {
	withDocumentation, ok := api.(WithAPIDocumentation)
	if !ok {
		return APIDocumentation{}, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("koba: APIDocumentation panicked while describing API: %v", recovered)
		}
	}()
	return withDocumentation.APIDocumentation(), nil
}

func bodyInputType(inputTypes []reflect.Type) reflect.Type {
	if len(inputTypes) == 0 {
		return nil
	}
	start := 0
	if HasContext(inputTypes) {
		start = 1
	}
	if len(inputTypes) == start {
		return nil
	}
	return inputTypes[start]
}

// ValidateAPIDescription checks the transport-neutral contract without
// requiring the original API value. Generated plans and OpenAPI callers use
// it to enforce the same invariants as reflection.
func ValidateAPIDescription(description APIDescription) error {
	return validateOperationDescriptions(description.Operations)
}

func validateOperationDescriptions(operations []OperationDescription) error {
	operationIDs := make(map[string]string, len(operations))
	for _, operation := range operations {
		switch operation.HTTPMethod {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return fmt.Errorf(
				"koba: %s has unsupported HTTP method %q; supported methods are GET, POST, PUT, PATCH, and DELETE",
				operation.MethodName,
				operation.HTTPMethod,
			)
		}
		if previous, duplicate := operationIDs[operation.OperationID]; duplicate {
			return fmt.Errorf("koba: duplicate operation ID %q on %s and %s", operation.OperationID, previous, operation.MethodName)
		}
		operationIDs[operation.OperationID] = operation.MethodName
		seenParameters := make(map[string]struct{}, len(operation.Documentation.Parameters))
		for _, parameter := range operation.Documentation.Parameters {
			if strings.TrimSpace(parameter.Name) == "" {
				return fmt.Errorf("koba: %s has a parameter with an empty name", operation.MethodName)
			}
			switch parameter.In {
			case ParameterInQuery, ParameterInHeader, ParameterInCookie, ParameterInPath:
			default:
				return fmt.Errorf("koba: %s parameter %s has invalid location %q", operation.MethodName, parameter.Name, parameter.In)
			}
			if parameter.GoType == nil {
				return fmt.Errorf("koba: %s parameter %s has no Go type", operation.MethodName, parameter.Name)
			}
			parameterName := parameter.Name
			if parameter.In == ParameterInHeader {
				parameterName = strings.ToLower(parameterName)
			}
			key := string(parameter.In) + ":" + parameterName
			if _, duplicate := seenParameters[key]; duplicate {
				return fmt.Errorf("koba: %s has duplicate parameter %s", operation.MethodName, key)
			}
			seenParameters[key] = struct{}{}
		}
		if operation.Documentation.Public && operation.Documentation.Security != nil {
			return fmt.Errorf("koba: %s cannot set both Public and Security", operation.MethodName)
		}
		if operation.Documentation.Security != nil && len(operation.Documentation.Security) == 0 {
			return fmt.Errorf("koba: %s has an empty Security override; use Public for an unauthenticated operation", operation.MethodName)
		}
		for index, requirement := range operation.Documentation.Security {
			if len(requirement) == 0 {
				return fmt.Errorf("koba: %s security requirement %d is empty", operation.MethodName, index+1)
			}
		}
		successStatus := operation.ResolvedSuccessStatus()
		if successStatus < http.StatusOK || successStatus > 299 {
			return fmt.Errorf(
				"koba: %s success status must be between 200 and 299; got %d",
				operation.MethodName,
				successStatus,
			)
		}
		if (successStatus == http.StatusNoContent || successStatus == http.StatusResetContent) && operation.OutputType != nil {
			return fmt.Errorf(
				"koba: %s success status %d cannot be used by an operation with a response value",
				operation.MethodName,
				successStatus,
			)
		}
		for status := range operation.Documentation.Responses {
			if status == successStatus || (status >= 400 && status <= 599) {
				continue
			}
			return fmt.Errorf(
				"koba: %s documents HTTP %d, but its resolved success status is %d",
				operation.MethodName,
				status,
				successStatus,
			)
		}
	}
	return nil
}

func validateDocumentationCoverage(documentation APIDocumentation, operations []OperationDescription) error {
	if err := validateOperationDescriptions(operations); err != nil {
		return err
	}
	known := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		known[operation.MethodName] = struct{}{}
	}
	for method := range documentation.Operations {
		if _, exists := known[method]; !exists {
			return fmt.Errorf("koba: documentation references unknown API method %q", method)
		}
	}
	return nil
}
