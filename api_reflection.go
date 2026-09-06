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
	description.Document = cloneAPIDocument(documentation.Document)
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
		results, signatureErr := analyzeOperationResults(outputTypes)
		if signatureErr != nil {
			return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, signatureErr)
		}
		operationContract := contract.Operations[method.Name]
		operationDocumentation := documentation.Operations[method.Name]
		operationID := strings.TrimSpace(operationDocumentation.OperationID)
		if operationID == "" {
			operationID = defaultOperationID(typeOfAPI, method.Name)
		}
		operation := OperationDescription{
			MethodName:    method.Name,
			OperationID:   operationID,
			Documentation: operationDocumentation,
			Contract:      operationContract,
		}

		if operation.HasKafkaConsumerTransport() {
			if _, routeIsExplicit := routes[method.Name]; routeIsExplicit {
				return APIDescription{}, fmt.Errorf(
					"koba: describe %s: Kafka consumer operation cannot declare an HTTP route",
					method.Name,
				)
			}
			messageType, signatureErr := kafkaConsumerSignature(inputTypes, results)
			if signatureErr != nil {
				return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, signatureErr)
			}
			operation.InputType = messageType
			description.Operations = append(description.Operations, operation)
			continue
		}

		post, methodErr := isPost(inputTypes)
		if methodErr != nil {
			return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, methodErr)
		}

		httpMethod := http.MethodGet
		if post {
			httpMethod = http.MethodPost
		}
		route, routeIsExplicit := routes[method.Name]
		documentedRoute := strings.TrimSpace(operationDocumentation.Route)
		if routeIsExplicit && documentedRoute != "" {
			return APIDescription{}, fmt.Errorf(
				"koba: describe %s: route is declared in both Routes and APIDocumentation",
				method.Name,
			)
		}
		if documentedRoute != "" {
			route = documentedRoute
			routeIsExplicit = true
		}
		if !routeIsExplicit {
			route = "/" + method.Name
		}
		if explicitMethod := strings.ToUpper(strings.TrimSpace(operationDocumentation.Method)); explicitMethod != "" {
			switch explicitMethod {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				httpMethod = explicitMethod
			default:
				return APIDescription{}, fmt.Errorf("koba: describe %s: unsupported HTTP method %q", method.Name, operationDocumentation.Method)
			}
		}
		operation.Route = route
		operation.HTTPMethod = httpMethod
		if bodyType := bodyInputType(inputTypes); bodyType != nil {
			operation.InputType = bodyType
		}
		operation.OutputType = results.OutputType
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
		if operation.Contract.Response != nil {
			bindings, bindingErr := normalizeResponseBindings(operation.Contract.Response, operation.OutputType)
			if bindingErr != nil {
				return APIDescription{}, fmt.Errorf("koba: describe %s: %w", method.Name, bindingErr)
			}
			operation.ResponseBindings = bindings
			// Retain only framework-owned compiled bindings in execution plans.
			operation.Contract.Response = nil
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

func kafkaConsumerSignature(inputTypes []reflect.Type, results operationResultSignature) (reflect.Type, error) {
	start := 0
	if HasContext(inputTypes) {
		start = 1
	}
	if len(inputTypes) != start+1 {
		return nil, fmt.Errorf("Kafka consumer operation requires exactly one message argument, with an optional context.Context")
	}
	messageType := inputTypes[start]
	if _, err := inferKafkaMessageEncoding(messageType); err != nil {
		return nil, err
	}
	if results.OutputType != nil {
		return nil, fmt.Errorf("Kafka consumer operation may only return error")
	}
	return messageType, nil
}

func cloneAPIDocument(document *APIDocument) *APIDocument {
	if document == nil {
		return nil
	}
	cloned := *document
	cloned.Servers = append([]APIServer(nil), document.Servers...)
	cloned.SecuritySchemes = append([]APISecurityScheme(nil), document.SecuritySchemes...)
	if document.DefaultSecurity != nil {
		cloned.DefaultSecurity = make([]SecurityRequirement, len(document.DefaultSecurity))
		for index, requirement := range document.DefaultSecurity {
			cloned.DefaultSecurity[index] = append(SecurityRequirement(nil), requirement...)
		}
	}
	return &cloned
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
