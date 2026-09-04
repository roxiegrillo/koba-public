package staticgen

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"

	fb "github.com/roxiegrillo/koba/v2"
)

func writeAPI(source *bytes.Buffer, imports *importSet, apiType reflect.Type, description fb.APIDescription) error {
	receiverName := apiType.Name()
	receiverExpression := receiverName
	if apiType.Kind() == reflect.Pointer {
		receiverName = apiType.Elem().Name()
		receiverExpression = "*" + receiverName
	}
	fmt.Fprintf(source, "func (api %s) KobaCompiledAPI() fb.CompiledAPI {\n", receiverExpression)
	fmt.Fprintf(source, "\treturn fb.CompiledAPI{\n")
	fmt.Fprintf(source, "\t\tDescription: ")
	if err := writeDescription(source, imports, description); err != nil {
		return err
	}
	fmt.Fprintf(source, ",\n")
	fmt.Fprintf(source, "\t\tInvokers: map[string]fb.OperationInvoker{\n")
	for _, operation := range description.Operations {
		method, _ := apiType.MethodByName(operation.MethodName)
		inputs, outputs := fb.GetInputOutputTypes(method)
		fmt.Fprintf(source, "\t\t\t%q: func(invocation fb.Invocation) (fb.InvocationResult, error) {\n", operation.MethodName)
		body := bodyType(inputs)
		argument := ""
		if body != nil {
			expression, err := imports.typeExpression(body)
			if err != nil {
				return err
			}
			if body.Kind() == reflect.Pointer {
				elementExpression, expressionErr := imports.typeExpression(body.Elem())
				if expressionErr != nil {
					return expressionErr
				}
				argument = "request"
				fmt.Fprintf(source, "\t\t\t\tvar request %s\n", expression)
				fmt.Fprintf(source, "\t\t\t\tif invocation.HasInput {\n")
				fmt.Fprintf(source, "\t\t\t\t\tif err := invocation.Decode(&request); err != nil { return fb.InvocationResult{}, err }\n")
				fmt.Fprintf(source, "\t\t\t\t} else {\n")
				fmt.Fprintf(source, "\t\t\t\t\trequest = new(%s)\n", elementExpression)
				fmt.Fprintf(source, "\t\t\t\t\tif err := invocation.Decode(request); err != nil { return fb.InvocationResult{}, err }\n")
				fmt.Fprintf(source, "\t\t\t\t}\n")
			} else {
				argument = "request"
				fmt.Fprintf(source, "\t\t\t\tvar request %s\n", expression)
				fmt.Fprintf(source, "\t\t\t\tif err := invocation.Decode(&request); err != nil { return fb.InvocationResult{}, err }\n")
			}
		}
		arguments := make([]string, 0, 2)
		if fb.HasContext(inputs) {
			arguments = append(arguments, "invocation.Context")
		}
		if argument != "" {
			arguments = append(arguments, argument)
		}
		call := fmt.Sprintf("api.%s(%s)", operation.MethodName, strings.Join(arguments, ", "))
		if len(outputs) == 0 {
			fmt.Fprintf(source, "\t\t\t\t%s\n", call)
			fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{}, nil\n")
		} else {
			fmt.Fprintf(source, "\t\t\t\tvalue := %s\n", call)
			if outputs[0].Kind() == reflect.Pointer {
				fmt.Fprintf(source, "\t\t\t\tif value == nil { return fb.InvocationResult{Value: nil, HasValue: true}, nil }\n")
				fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{Value: *value, HasValue: true}, nil\n")
			} else {
				fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{Value: value, HasValue: true}, nil\n")
			}
		}
		fmt.Fprintf(source, "\t\t\t},\n")
	}
	fmt.Fprintf(source, "\t\t},\n")
	fmt.Fprintf(source, "\t}\n")
	fmt.Fprintf(source, "}\n\n")
	return nil
}

func writeDescription(source *bytes.Buffer, imports *importSet, description fb.APIDescription) error {
	fmt.Fprintf(source, "fb.APIDescription{Name: %q, Description: %q, Operations: []fb.OperationDescription{", description.Name, description.Description)
	for _, operation := range description.Operations {
		fmt.Fprintf(source, "{MethodName: %q, OperationID: %q, Route: %q, HTTPMethod: %q", operation.MethodName, operation.OperationID, operation.Route, operation.HTTPMethod)
		if operation.InputType != nil {
			expression, err := imports.typeExpression(operation.InputType)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, ", InputType: reflect.TypeFor[%s]()", expression)
		}
		if operation.OutputType != nil {
			expression, err := imports.typeExpression(operation.OutputType)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, ", OutputType: reflect.TypeFor[%s]()", expression)
		}
		if len(operation.Contract.Inputs) != 0 || len(operation.Contract.Authorization.RequiredScopes) != 0 {
			fmt.Fprintf(source, ", Contract: ")
			if err := writeOperationContract(source, imports, operation.Contract); err != nil {
				return err
			}
		}
		if len(operation.RequestBindings) != 0 {
			fmt.Fprintf(source, ", RequestBindings: ")
			if err := writeRequestBindings(source, imports, operation.RequestBindings); err != nil {
				return err
			}
		}
		fmt.Fprintf(source, ", Documentation: ")
		if err := writeOperationDocumentation(source, imports, operation.Documentation); err != nil {
			return err
		}
		fmt.Fprintf(source, "},")
	}
	fmt.Fprintf(source, "}}")
	return nil
}

func writeOperationContract(source *bytes.Buffer, imports *importSet, contract fb.OperationContract) error {
	identifiers := make([]string, 0, len(contract.Inputs))
	for identifier := range contract.Inputs {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	fmt.Fprintf(source, "fb.OperationContract{")
	if len(identifiers) > 0 {
		fmt.Fprintf(source, "Inputs: map[string]fb.InputContract{")
	}
	for _, identifier := range identifiers {
		input := contract.Inputs[identifier]
		fmt.Fprintf(source, "%q: ", identifier)
		if err := writeInputContract(source, imports, input, false); err != nil {
			return err
		}
		fmt.Fprintf(source, ",")
	}
	if len(identifiers) > 0 {
		fmt.Fprintf(source, "}")
	}
	if contract.Authorization.RequiredScopes != nil {
		if len(identifiers) > 0 {
			fmt.Fprintf(source, ", ")
		}
		fmt.Fprintf(
			source,
			"Authorization: fb.AuthorizationContract{RequiredScopes: %#v}",
			contract.Authorization.RequiredScopes,
		)
	}
	fmt.Fprintf(source, "}")
	return nil
}

func writeRequestBindings(source *bytes.Buffer, imports *importSet, bindings []fb.RequestBinding) error {
	fmt.Fprintf(source, "[]fb.RequestBinding{")
	for _, binding := range bindings {
		fmt.Fprintf(source, "{Source: fb.BindingSource(%q), Name: %q", binding.Source, binding.Name)
		if binding.FieldPath != nil {
			fmt.Fprintf(source, ", FieldPath: %#v", binding.FieldPath)
		}
		if binding.FieldIndex != nil {
			fmt.Fprintf(source, ", FieldIndex: %#v", binding.FieldIndex)
		}
		fmt.Fprintf(source, ", Input: ")
		if err := writeInputContract(source, imports, binding.Input, true); err != nil {
			return err
		}
		fmt.Fprintf(source, "},")
	}
	fmt.Fprintf(source, "}")
	return nil
}

func writeInputContract(source *bytes.Buffer, imports *importSet, input fb.InputContract, qualified bool) error {
	typeExpression, err := imports.typeExpression(input.GoType)
	if err != nil {
		return err
	}
	if qualified {
		fmt.Fprintf(source, "fb.InputContract")
	}
	fmt.Fprintf(source, "{GoType: reflect.TypeFor[%s]()", typeExpression)
	if input.Required {
		fmt.Fprintf(source, ", Required: true")
	}
	if input.HasDefault {
		literal, err := typedContractLiteral(input.Default, input.GoType, imports)
		if err != nil {
			return err
		}
		fmt.Fprintf(source, ", Default: %s, HasDefault: true", literal)
	}
	constraintType := input.GoType
	for constraintType.Kind() == reflect.Pointer {
		constraintType = constraintType.Elem()
	}
	for _, constraint := range []struct {
		name  string
		value any
	}{{"Minimum", input.Minimum}, {"Maximum", input.Maximum}} {
		name, value := constraint.name, constraint.value
		if value == nil {
			continue
		}
		literal, err := typedContractLiteral(value, constraintType, imports)
		if err != nil {
			return err
		}
		fmt.Fprintf(source, ", %s: %s", name, literal)
	}
	if input.MinLength != nil {
		fmt.Fprintf(source, ", MinLength: func() *uint64 { value := uint64(%d); return &value }()", *input.MinLength)
	}
	if input.MaxLength != nil {
		fmt.Fprintf(source, ", MaxLength: func() *uint64 { value := uint64(%d); return &value }()", *input.MaxLength)
	}
	if input.Enum != nil {
		fmt.Fprintf(source, ", Enum: []any{")
		for _, value := range input.Enum {
			literal, err := typedContractLiteral(value, constraintType, imports)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, "%s,", literal)
		}
		fmt.Fprintf(source, "}")
	}
	if input.Pattern != "" {
		fmt.Fprintf(source, ", Pattern: %q", input.Pattern)
	}
	if input.Format != "" {
		fmt.Fprintf(source, ", Format: %q", input.Format)
	}
	fmt.Fprintf(source, "}")
	return nil
}

func typedContractLiteral(value any, target reflect.Type, imports *importSet) (string, error) {
	literal, err := anyLiteral(value)
	if err != nil {
		return "", err
	}
	if target.Name() == "" || target.PkgPath() == "" {
		return literal, nil
	}
	typeExpression, err := imports.typeExpression(target)
	if err != nil {
		return "", err
	}
	return typeExpression + "(" + literal + ")", nil
}

func writeOperationDocumentation(source *bytes.Buffer, imports *importSet, documentation fb.OperationDocumentation) error {
	fmt.Fprintf(source, "fb.OperationDocumentation{Method: %q, SuccessStatus: %d, OperationID: %q, Summary: %q, Description: %q", documentation.Method, documentation.SuccessStatus, documentation.OperationID, documentation.Summary, documentation.Description)
	if documentation.Tags != nil {
		fmt.Fprintf(source, ", Tags: %#v", documentation.Tags)
	}
	if documentation.Parameters != nil {
		fmt.Fprintf(source, ", Parameters: []fb.ParameterDocumentation{")
		for _, parameter := range documentation.Parameters {
			fmt.Fprintf(source, "{Name: %q, In: fb.ParameterLocation(%q)", parameter.Name, parameter.In)
			if parameter.ContractInput != "" {
				fmt.Fprintf(source, ", ContractInput: %q", parameter.ContractInput)
			}
			fmt.Fprintf(source, ", Description: %q, Required: %t, Deprecated: %t", parameter.Description, parameter.Required, parameter.Deprecated)
			if parameter.GoType != nil {
				expression, err := imports.typeExpression(parameter.GoType)
				if err != nil {
					return err
				}
				fmt.Fprintf(source, ", GoType: reflect.TypeFor[%s]()", expression)
			}
			fmt.Fprintf(source, ", Format: %q, Pattern: %q", parameter.Format, parameter.Pattern)
			if parameter.Enum != nil {
				fmt.Fprintf(source, ", Enum: []any{")
				for _, value := range parameter.Enum {
					literal, err := anyLiteral(value)
					if err != nil {
						return err
					}
					fmt.Fprintf(source, "%s,", literal)
				}
				fmt.Fprintf(source, "}")
			}
			if parameter.Example != nil {
				literal, err := anyLiteral(parameter.Example)
				if err != nil {
					return err
				}
				fmt.Fprintf(source, ", Example: %s", literal)
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.RequestBodyDescription != "" {
		fmt.Fprintf(source, ", RequestBodyDescription: %q", documentation.RequestBodyDescription)
	}
	if documentation.Responses != nil {
		statuses := make([]int, 0, len(documentation.Responses))
		for status := range documentation.Responses {
			statuses = append(statuses, status)
		}
		sort.Ints(statuses)
		fmt.Fprintf(source, ", Responses: map[int]fb.ResponseDocumentation{")
		for _, status := range statuses {
			response := documentation.Responses[status]
			fmt.Fprintf(source, "%d: {Description: %q", status, response.Description)
			if response.Headers != nil {
				headerNames := make([]string, 0, len(response.Headers))
				for name := range response.Headers {
					headerNames = append(headerNames, name)
				}
				sort.Strings(headerNames)
				fmt.Fprintf(source, ", Headers: map[string]fb.HeaderDocumentation{")
				for _, name := range headerNames {
					header := response.Headers[name]
					fmt.Fprintf(source, "%q: {Description: %q, Required: %t", name, header.Description, header.Required)
					if header.Format != "" {
						fmt.Fprintf(source, ", Format: %q", header.Format)
					}
					if header.Pattern != "" {
						fmt.Fprintf(source, ", Pattern: %q", header.Pattern)
					}
					if header.Enum != nil {
						fmt.Fprintf(source, ", Enum: %#v", header.Enum)
					}
					if header.Example != "" {
						fmt.Fprintf(source, ", Example: %q", header.Example)
					}
					fmt.Fprintf(source, "},")
				}
				fmt.Fprintf(source, "}")
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.Deprecated {
		fmt.Fprintf(source, ", Deprecated: true")
	}
	if documentation.RequiredScopes != nil {
		fmt.Fprintf(source, ", RequiredScopes: %#v", documentation.RequiredScopes)
	}
	if documentation.Security != nil {
		fmt.Fprintf(source, ", Security: []fb.SecurityRequirement{")
		for _, requirement := range documentation.Security {
			fmt.Fprintf(source, "{")
			for _, scheme := range requirement {
				fmt.Fprintf(source, "%q,", scheme)
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.Public {
		fmt.Fprintf(source, ", Public: true")
	}
	fmt.Fprintf(source, "}")
	return nil
}

func bodyType(inputs []reflect.Type) reflect.Type {
	if len(inputs) == 0 {
		return nil
	}
	start := 0
	if fb.HasContext(inputs) {
		start = 1
	}
	if len(inputs) == start {
		return nil
	}
	return inputs[start]
}
