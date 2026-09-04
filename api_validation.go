package koba

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// ValidateAPIDescription checks the resolved structural and HTTP metadata
// without requiring the original API value. Generated plans and OpenAPI callers
// use it to enforce the same invariants as reflection.
func ValidateAPIDescription(description APIDescription) error {
	return validateOperationDescriptions(description.Operations)
}

func validateOperationDescriptions(operations []OperationDescription) error {
	operationIDs := make(map[string]string, len(operations))
	for _, operation := range operations {
		if operation.Contract.Request != nil {
			return fmt.Errorf("koba: %s contains an unresolved request companion", operation.MethodName)
		}
		if len(operation.Contract.Inputs) != 0 && len(operation.RequestBindings) != 0 {
			return fmt.Errorf("koba: %s mixes legacy contract inputs with request bindings", operation.MethodName)
		}
		if _, err := normalizeResolvedRequestBindings(operation.RequestBindings, operation.InputType); err != nil {
			return fmt.Errorf("koba: %s: %w", operation.MethodName, err)
		}
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
		boundInputs := make(map[string]struct{}, len(operation.Contract.Inputs))
		for _, identifier := range sortedMapKeys(operation.Contract.Inputs) {
			input := operation.Contract.Inputs[identifier]
			if strings.TrimSpace(identifier) == "" {
				return fmt.Errorf("koba: %s has an empty contract input identifier", operation.MethodName)
			}
			if _, err := normalizeInputContract(input); err != nil {
				return fmt.Errorf("koba: %s input %s: %w", operation.MethodName, identifier, err)
			}
		}
		for _, parameter := range operation.Documentation.Parameters {
			if strings.TrimSpace(parameter.Name) == "" {
				return fmt.Errorf("koba: %s has a parameter with an empty name", operation.MethodName)
			}
			switch parameter.In {
			case ParameterInQuery, ParameterInHeader, ParameterInCookie, ParameterInPath:
			default:
				return fmt.Errorf("koba: %s parameter %s has invalid location %q", operation.MethodName, parameter.Name, parameter.In)
			}
			requestBinding, hasRequestBinding := requestParameterBinding(operation, parameter)
			if len(operation.RequestBindings) != 0 && !hasRequestBinding {
				return fmt.Errorf(
					"koba: %s parameter %s has no request binding",
					operation.MethodName,
					parameter.Name,
				)
			}
			if parameter.ContractInput == "" && parameter.GoType == nil && !hasRequestBinding {
				return fmt.Errorf("koba: %s parameter %s has no Go type", operation.MethodName, parameter.Name)
			}
			if parameter.ContractInput != "" && hasRequestBinding {
				return fmt.Errorf(
					"koba: %s parameter %s binds both a request field and a legacy contract input",
					operation.MethodName,
					parameter.Name,
				)
			}
			if parameter.ContractInput != "" {
				input, exists := operation.Contract.Inputs[parameter.ContractInput]
				if !exists {
					return fmt.Errorf(
						"koba: %s parameter %s references unknown contract input %q",
						operation.MethodName,
						parameter.Name,
						parameter.ContractInput,
					)
				}
				if parameter.GoType != nil || parameter.Required || parameter.Format != "" || parameter.Pattern != "" || len(parameter.Enum) != 0 {
					return fmt.Errorf(
						"koba: %s parameter %s mixes ContractInput with legacy structural fields",
						operation.MethodName,
						parameter.Name,
					)
				}
				if err := validateHTTPScalarType(input.GoType); err != nil {
					return fmt.Errorf("koba: %s parameter %s: %w", operation.MethodName, parameter.Name, err)
				}
				if _, duplicate := boundInputs[parameter.ContractInput]; duplicate {
					return fmt.Errorf("koba: %s contract input %q is bound more than once", operation.MethodName, parameter.ContractInput)
				}
				if parameter.In == ParameterInPath && !input.Required {
					return fmt.Errorf("koba: %s path parameter %s must bind a required contract input", operation.MethodName, parameter.Name)
				}
				boundInputs[parameter.ContractInput] = struct{}{}
			}
			if hasRequestBinding {
				if parameter.GoType != nil && parameter.GoType != requestBinding.Input.GoType {
					return fmt.Errorf(
						"koba: %s parameter %s documents type %s, but its request binding uses %s",
						operation.MethodName,
						parameter.Name,
						parameter.GoType,
						requestBinding.Input.GoType,
					)
				}
				if parameter.Required && !requestBinding.Input.Required {
					return fmt.Errorf(
						"koba: %s parameter %s is documented as required, but its request binding is optional",
						operation.MethodName,
						parameter.Name,
					)
				}
			}
			if parameter.Example != nil {
				input := InputContract{
					GoType:  parameter.GoType,
					Pattern: parameter.Pattern,
					Enum:    parameter.Enum,
				}
				if parameter.ContractInput != "" {
					input = operation.Contract.Inputs[parameter.ContractInput]
				} else if hasRequestBinding {
					input = requestBinding.Input
				}
				normalizedInput, err := normalizeInputContract(input)
				if err != nil {
					return fmt.Errorf("koba: %s parameter %s example contract: %w", operation.MethodName, parameter.Name, err)
				}
				example, err := normalizeContractValue(parameter.Example, normalizedInput.GoType)
				if err != nil {
					return fmt.Errorf("koba: %s parameter %s example: %w", operation.MethodName, parameter.Name, err)
				}
				if err := validateInputValue(normalizedInput, example); err != nil {
					return fmt.Errorf("koba: %s parameter %s example: %w", operation.MethodName, parameter.Name, err)
				}
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
		for _, identifier := range sortedMapKeys(operation.Contract.Inputs) {
			if _, exists := boundInputs[identifier]; !exists {
				return fmt.Errorf("koba: %s contract input %q has no HTTP parameter binding", operation.MethodName, identifier)
			}
		}
		if operation.Documentation.Public && operation.Documentation.Security != nil {
			return fmt.Errorf("koba: %s cannot set both Public and Security", operation.MethodName)
		}
		contractScopes := operation.Contract.Authorization.RequiredScopes
		legacyScopes := operation.Documentation.RequiredScopes
		if len(contractScopes) > 0 && len(legacyScopes) > 0 {
			return fmt.Errorf(
				"koba: %s declares RequiredScopes in both APIContract and APIDocumentation",
				operation.MethodName,
			)
		}
		requiredScopes := operation.ResolvedRequiredScopes()
		if operation.Documentation.Public && len(requiredScopes) > 0 {
			return fmt.Errorf("koba: %s cannot set both Public and RequiredScopes", operation.MethodName)
		}
		if err := validateRequiredScopes(requiredScopes); err != nil {
			return fmt.Errorf("koba: %s %w", operation.MethodName, err)
		}
		for index, requirement := range operation.Documentation.Security {
			if len(requirement) == 0 && len(requiredScopes) > 0 {
				return fmt.Errorf("koba: %s security requirement %d allows anonymous access but RequiredScopes is nonempty", operation.MethodName, index+1)
			}
		}
		if operation.Documentation.Security != nil && len(operation.Documentation.Security) == 0 && len(requiredScopes) > 0 {
			return fmt.Errorf("koba: %s has an empty Security override but RequiredScopes is nonempty", operation.MethodName)
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
		for status, response := range operation.Documentation.Responses {
			if status == successStatus || (status >= 400 && status <= 599) {
				if err := validateResponseHeaders(operation.MethodName, status, response.Headers); err != nil {
					return err
				}
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

func validateResponseHeaders(methodName string, status int, headers map[string]HeaderDocumentation) error {
	seen := make(map[string]string, len(headers))
	for name, header := range headers {
		if !validHTTPHeaderName(name) {
			return fmt.Errorf("koba: %s response HTTP %d has invalid header name %q", methodName, status, name)
		}
		key := strings.ToLower(name)
		if previous, duplicate := seen[key]; duplicate {
			return fmt.Errorf("koba: %s response HTTP %d has duplicate header names %q and %q", methodName, status, previous, name)
		}
		seen[key] = name
		if header.Pattern != "" {
			if _, err := regexp.Compile(header.Pattern); err != nil {
				return fmt.Errorf(
					"koba: %s response HTTP %d header %q has invalid pattern: %w",
					methodName,
					status,
					name,
					err,
				)
			}
		}
		enumValues := make(map[string]struct{}, len(header.Enum))
		for _, value := range header.Enum {
			if value == "" {
				return fmt.Errorf("koba: %s response HTTP %d header %q has an empty enum value", methodName, status, name)
			}
			if _, duplicate := enumValues[value]; duplicate {
				return fmt.Errorf("koba: %s response HTTP %d header %q has duplicate enum value %q", methodName, status, name, value)
			}
			enumValues[value] = struct{}{}
		}
		if header.Example != "" {
			if header.Pattern != "" {
				matches, _ := regexp.MatchString(header.Pattern, header.Example)
				if !matches {
					return fmt.Errorf("koba: %s response HTTP %d header %q example does not match its pattern", methodName, status, name)
				}
			}
			if len(header.Enum) > 0 {
				if _, exists := enumValues[header.Example]; !exists {
					return fmt.Errorf("koba: %s response HTTP %d header %q example is not in its enum", methodName, status, name)
				}
			}
		}
	}
	return nil
}

func requestParameterBinding(
	operation OperationDescription,
	parameter ParameterDocumentation,
) (RequestBinding, bool) {
	source := BindingSource(parameter.In)
	for _, binding := range operation.RequestBindings {
		if binding.Source != source {
			continue
		}
		if source == BindingSourceHeader {
			if strings.EqualFold(binding.Name, parameter.Name) {
				return binding, true
			}
			continue
		}
		if binding.Name == parameter.Name {
			return binding, true
		}
	}
	return RequestBinding{}, false
}

func validHTTPHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if ('a' <= character && character <= 'z') ||
			('A' <= character && character <= 'Z') ||
			('0' <= character && character <= '9') {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func validOAuthScope(scope string) bool {
	if scope == "" {
		return false
	}
	for _, character := range scope {
		if character < 0x21 || character > 0x7e || character == '"' || character == '\\' {
			return false
		}
	}
	return true
}

func validateRequiredScopes(scopes []string) error {
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope == "" {
			return fmt.Errorf("has an empty required scope")
		}
		if !validOAuthScope(scope) {
			return fmt.Errorf("has invalid required scope %q", scope)
		}
		if _, duplicate := seen[scope]; duplicate {
			return fmt.Errorf("has duplicate required scope %q", scope)
		}
		seen[scope] = struct{}{}
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

func validateContractCoverage(contract APIContract, operations []OperationDescription) error {
	known := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		known[operation.MethodName] = struct{}{}
	}
	for _, method := range sortedMapKeys(contract.Operations) {
		if _, exists := known[method]; !exists {
			return fmt.Errorf("koba: API contract references unknown API method %q", method)
		}
	}
	return nil
}
