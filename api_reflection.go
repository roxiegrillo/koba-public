package koba

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
)

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
	contract, err := apiContract(api)
	if err != nil {
		return APIDescription{}, err
	}

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
			Contract:      contract.Operations[method.Name],
		}
		if bodyType := bodyInputType(inputTypes); bodyType != nil {
			operation.InputType = bodyType
		}
		if operation.Contract.Request != nil {
			if len(operation.Contract.Inputs) != 0 {
				return APIDescription{}, fmt.Errorf(
					"koba: describe %s: request descriptor cannot be combined with legacy contract inputs",
					method.Name,
				)
			}
			bindings, bindingErr := normalizeRequestBindings(operation.Contract.Request, operation.InputType)
			if bindingErr != nil {
				return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, bindingErr)
			}
			operation.RequestBindings = bindings
			// The arbitrary companion value is authoring syntax. The resolved plan
			// stores only framework-owned bindings so it remains serializable by
			// static generation.
			operation.Contract.Request = nil
		}
		if len(outputTypes) == 1 {
			operation.OutputType = outputTypes[0]
		}
		description.Operations = append(description.Operations, operation)
	}

	if err := validateDocumentationCoverage(documentation, description.Operations); err != nil {
		return APIDescription{}, err
	}
	if err := validateContractCoverage(contract, description.Operations); err != nil {
		return APIDescription{}, err
	}
	return description, nil
}

func apiContract(api any) (contract APIContract, err error) {
	withContract, ok := api.(WithAPIContract)
	if !ok {
		return APIContract{}, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("koba: APIContract panicked while describing API: %v", recovered)
		}
	}()
	contract, err = normalizeAPIContract(withContract.APIContract())
	return contract, err
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
