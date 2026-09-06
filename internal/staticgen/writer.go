package staticgen

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

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
		returnsError := len(outputs) > 0 && outputs[len(outputs)-1] == reflect.TypeFor[error]()
		if returnsError && operation.OutputType != nil {
			fmt.Fprintf(source, "\t\t\t\tvalue, operationError := %s\n", call)
			fmt.Fprintf(source, "\t\t\t\tif operationError != nil { return fb.InvocationResult{}, operationError }\n")
		} else if returnsError {
			fmt.Fprintf(source, "\t\t\t\tif operationError := %s; operationError != nil { return fb.InvocationResult{}, operationError }\n", call)
			fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{}, nil\n")
			fmt.Fprintf(source, "\t\t\t},\n")
			continue
		} else if operation.OutputType == nil {
			fmt.Fprintf(source, "\t\t\t\t%s\n", call)
			fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{}, nil\n")
		} else {
			fmt.Fprintf(source, "\t\t\t\tvalue := %s\n", call)
		}
		if operation.OutputType != nil {
			if operation.OutputType.Kind() == reflect.Pointer {
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
	fmt.Fprintf(source, "fb.APIDescription{Name: %q, Description: %q", description.Name, description.Description)
	if description.Document != nil {
		fmt.Fprintf(source, ", Document: ")
		writeAPIDocument(source, description.Document)
	}
	fmt.Fprintf(source, ", Operations: []fb.OperationDescription{")
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
		if len(operation.Contract.Inputs) != 0 || len(operation.Contract.Authorization.RequiredScopes) != 0 || operation.Contract.Transport.WebSocket != nil || operation.Contract.Transport.KafkaConsumer != nil {
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
		if len(operation.ResponseBindings) != 0 {
			fmt.Fprintf(source, ", ResponseBindings: ")
			if err := writeResponseBindings(source, imports, operation.ResponseBindings); err != nil {
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

func writeAPIDocument(source *bytes.Buffer, document *fb.APIDocument) {
	fmt.Fprintf(
		source,
		"&fb.APIDocument{Title: %q, Version: %q, Description: %q, ExternalDocsURL: %q",
		document.Title,
		document.Version,
		document.Description,
		document.ExternalDocsURL,
	)
	if document.Servers != nil {
		fmt.Fprintf(source, ", Servers: []fb.APIServer{")
		for _, server := range document.Servers {
			fmt.Fprintf(source, "{URL: %q, Description: %q},", server.URL, server.Description)
		}
		fmt.Fprintf(source, "}")
	}
	if document.SecuritySchemes != nil {
		fmt.Fprintf(source, ", SecuritySchemes: []fb.APISecurityScheme{")
		for _, scheme := range document.SecuritySchemes {
			fmt.Fprintf(
				source,
				"{Name: %q, Type: %q, Description: %q, In: %q, Parameter: %q, Scheme: %q, BearerFormat: %q, ValuePrefix: %q},",
				scheme.Name,
				scheme.Type,
				scheme.Description,
				scheme.In,
				scheme.Parameter,
				scheme.Scheme,
				scheme.BearerFormat,
				scheme.ValuePrefix,
			)
		}
		fmt.Fprintf(source, "}")
	}
	if document.DefaultSecurity != nil {
		fmt.Fprintf(source, ", DefaultSecurity: []fb.SecurityRequirement{")
		for _, requirement := range document.DefaultSecurity {
			fmt.Fprintf(source, "{")
			for _, scheme := range requirement {
				fmt.Fprintf(source, "%q,", scheme)
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	fmt.Fprintf(source, "}")
}

func writeOperationContract(source *bytes.Buffer, imports *importSet, contract fb.OperationContract) error {
	identifiers := make([]string, 0, len(contract.Inputs))
	for identifier := range contract.Inputs {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	fmt.Fprintf(source, "fb.OperationContract{")
	wroteField := false
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
		wroteField = true
	}
	if contract.Authorization.RequiredScopes != nil {
		if wroteField {
			fmt.Fprintf(source, ", ")
		}
		fmt.Fprintf(
			source,
			"Authorization: fb.AuthorizationContract{RequiredScopes: %#v}",
			contract.Authorization.RequiredScopes,
		)
		wroteField = true
	}
	if contract.Transport.WebSocket != nil || contract.Transport.KafkaConsumer != nil {
		if wroteField {
			fmt.Fprintf(source, ", ")
		}
		if err := writeTransportContract(source, imports, contract.Transport); err != nil {
			return err
		}
	}
	fmt.Fprintf(source, "}")
	return nil
}

func writeTransportContract(source *bytes.Buffer, imports *importSet, contract fb.TransportContract) error {
	if consumer := contract.KafkaConsumer; consumer != nil {
		fmt.Fprintf(source, "Transport: fb.KafkaConsumer(%q)", consumer.Topic)
		return nil
	}
	webSocket := contract.WebSocket
	if webSocket == nil {
		return nil
	}
	clientType, err := imports.typeExpression(webSocket.Socket.ClientToServerType)
	if err != nil {
		return err
	}
	serverType, err := imports.typeExpression(webSocket.Socket.ServerToClientType)
	if err != nil {
		return err
	}
	if webSocket.Socket.ClientToServer == nil && webSocket.Socket.ServerToClient == nil {
		fmt.Fprintf(
			source,
			"Transport: fb.WebSocket(fb.Socket[%s, %s]())",
			clientType,
			serverType,
		)
		return nil
	}
	fmt.Fprintf(
		source,
		"Transport: fb.WebSocket(fb.SocketContract{ClientToServerType: reflect.TypeFor[%s](), ServerToClientType: reflect.TypeFor[%s]()",
		clientType,
		serverType,
	)
	for _, message := range []struct {
		name     string
		contract *fb.SocketMessageContract
	}{
		{name: "ClientToServer", contract: webSocket.Socket.ClientToServer},
		{name: "ServerToClient", contract: webSocket.Socket.ServerToClient},
	} {
		if message.contract == nil {
			continue
		}
		fmt.Fprintf(source, ", %s: &fb.SocketMessageContract{Description: %q", message.name, message.contract.Description)
		if message.contract.Bindings != nil {
			fmt.Fprintf(source, ", Bindings: ")
			if err := writeResponseBindings(source, imports, message.contract.Bindings); err != nil {
				return err
			}
		}
		fmt.Fprintf(source, "}")
	}
	fmt.Fprintf(source, "})")
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
		if binding.Documentation != nil {
			fmt.Fprintf(
				source,
				", Documentation: &fb.BindingDocumentation{Description: %q",
				binding.Documentation.Description,
			)
			if binding.Documentation.Deprecated {
				fmt.Fprintf(source, ", Deprecated: true")
			}
			if binding.Documentation.Example != nil {
				literal, err := documentationExampleLiteral(
					binding.Documentation.Example,
					binding.Input.GoType,
					imports,
				)
				if err != nil {
					return fmt.Errorf("binding %s %q documentation example: %w", binding.Source, binding.Name, err)
				}
				fmt.Fprintf(source, ", Example: %s", literal)
			}
			fmt.Fprintf(source, "}")
		}
		if len(binding.ObjectRules) != 0 {
			fmt.Fprintf(source, ", ObjectRules: ")
			if err := writeObjectRules(source, binding.ObjectRules); err != nil {
				return fmt.Errorf("binding %s %q object rules: %w", binding.Source, binding.Name, err)
			}
		}
		fmt.Fprintf(source, "},")
	}
	fmt.Fprintf(source, "}")
	return nil
}

func writeResponseBindings(source *bytes.Buffer, imports *importSet, bindings []fb.ResponseBinding) error {
	fmt.Fprintf(source, "[]fb.ResponseBinding{")
	for _, binding := range bindings {
		fmt.Fprintf(source, "{FieldPath: %#v, FieldIndex: %#v", binding.FieldPath, binding.FieldIndex)
		if binding.ElementAt != nil {
			fmt.Fprintf(source, ", ElementAt: %#v", binding.ElementAt)
		}
		fmt.Fprintf(source, ", Input: ")
		if err := writeInputContract(source, imports, binding.Input, true); err != nil {
			return err
		}
		if binding.Documentation != nil {
			fmt.Fprintf(source, ", Documentation: &fb.BindingDocumentation{Description: %q", binding.Documentation.Description)
			if binding.Documentation.Deprecated {
				fmt.Fprintf(source, ", Deprecated: true")
			}
			if binding.Documentation.Example != nil {
				literal, err := documentationExampleLiteral(
					binding.Documentation.Example,
					binding.Input.GoType,
					imports,
				)
				if err != nil {
					return fmt.Errorf("response binding %s documentation example: %w", strings.Join(binding.FieldPath, "."), err)
				}
				fmt.Fprintf(source, ", Example: %s", literal)
			}
			fmt.Fprintf(source, "}")
		}
		fmt.Fprintf(source, "},")
	}
	fmt.Fprintf(source, "}")
	return nil
}

func writeBindingDocumentation(
	source *bytes.Buffer,
	imports *importSet,
	documentation *fb.BindingDocumentation,
	target reflect.Type,
) error {
	fmt.Fprintf(source, "&fb.BindingDocumentation{Description: %q", documentation.Description)
	if documentation.Deprecated {
		fmt.Fprintf(source, ", Deprecated: true")
	}
	if documentation.Example != nil {
		literal, err := documentationExampleLiteral(documentation.Example, target, imports)
		if err != nil {
			return err
		}
		fmt.Fprintf(source, ", Example: %s", literal)
	}
	fmt.Fprintf(source, "}")
	return nil
}

func documentationExampleLiteral(value any, target reflect.Type, imports *importSet) (string, error) {
	reflected := reflect.ValueOf(value)
	if target.Kind() == reflect.Interface {
		if !reflected.IsValid() {
			return "nil", nil
		}
		// ValueOf exposes the concrete dynamic value. Keeping that concrete type
		// is essential for defaults: collapsing a struct to map[string]any or a
		// typed nil pointer to nil changes reflection/static behavior.
		return documentationExampleLiteral(value, reflected.Type(), imports)
	}
	for reflected.IsValid() && reflected.Kind() == reflect.Interface {
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return "nil", nil
	}
	if target == reflect.TypeFor[time.Time]() {
		instant, ok := reflected.Interface().(time.Time)
		if !ok {
			return "", fmt.Errorf("example has type %s, want %s", reflected.Type(), target)
		}
		timePackage := imports.alias("time")
		location := ""
		switch instant.Location() {
		case time.UTC:
			location = timePackage + ".UTC"
		case time.Local:
			location = timePackage + ".Local"
		default:
			name, offset := instant.Zone()
			location = fmt.Sprintf("%s.FixedZone(%q, %d)", timePackage, name, offset)
		}
		return fmt.Sprintf(
			"%s.Date(%d, %d, %d, %d, %d, %d, %d, %s)",
			timePackage,
			instant.Year(),
			instant.Month(),
			instant.Day(),
			instant.Hour(),
			instant.Minute(),
			instant.Second(),
			instant.Nanosecond(),
			location,
		), nil
	}
	if target.Kind() == reflect.Pointer {
		if reflected.Kind() == reflect.Pointer {
			if reflected.IsNil() {
				targetExpression, err := imports.typeExpression(target)
				if err != nil {
					return "", err
				}
				return "(" + targetExpression + ")(nil)", nil
			}
			reflected = reflected.Elem()
		}
		elementLiteral, err := documentationExampleLiteral(reflected.Interface(), target.Elem(), imports)
		if err != nil {
			return "", err
		}
		targetExpression, err := imports.typeExpression(target)
		if err != nil {
			return "", err
		}
		elementExpression, err := imports.typeExpression(target.Elem())
		if err != nil {
			return "", err
		}
		if target.Name() != "" {
			return fmt.Sprintf(
				"func() %s { var value %s = %s; return %s(&value) }()",
				targetExpression,
				elementExpression,
				elementLiteral,
				targetExpression,
			), nil
		}
		return fmt.Sprintf(
			"func() %s { var value %s = %s; return &value }()",
			targetExpression,
			elementExpression,
			elementLiteral,
		), nil
	}
	for reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return "nil", nil
		}
		reflected = reflected.Elem()
	}

	switch target.Kind() {
	case reflect.Struct:
		if reflected.Kind() != reflect.Struct {
			return "", fmt.Errorf("example has type %s, want %s", reflected.Type(), target)
		}
		typeExpression, err := imports.typeExpression(target)
		if err != nil {
			return "", err
		}
		var result strings.Builder
		result.WriteString(typeExpression)
		result.WriteByte('{')
		for index := 0; index < target.NumField(); index++ {
			field := target.Field(index)
			if !field.IsExported() {
				continue
			}
			literal, literalErr := documentationExampleLiteral(
				reflected.Field(index).Interface(),
				field.Type,
				imports,
			)
			if literalErr != nil {
				return "", fmt.Errorf("field %s: %w", field.Name, literalErr)
			}
			fmt.Fprintf(&result, "%s:%s,", field.Name, literal)
		}
		result.WriteByte('}')
		return result.String(), nil
	case reflect.Slice, reflect.Array:
		if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
			return "", fmt.Errorf("example has type %s, want %s", reflected.Type(), target)
		}
		typeExpression, err := imports.typeExpression(target)
		if err != nil {
			return "", err
		}
		if reflected.Kind() == reflect.Slice && reflected.IsNil() {
			return typeExpression + "(nil)", nil
		}
		var result strings.Builder
		result.WriteString(typeExpression)
		result.WriteByte('{')
		for index := 0; index < reflected.Len(); index++ {
			literal, literalErr := documentationExampleLiteral(reflected.Index(index).Interface(), target.Elem(), imports)
			if literalErr != nil {
				return "", literalErr
			}
			result.WriteString(literal)
			result.WriteByte(',')
		}
		result.WriteByte('}')
		return result.String(), nil
	case reflect.Map:
		if reflected.Kind() != reflect.Map || target.Key().Kind() != reflect.String {
			return "", fmt.Errorf("example has type %s, want string-keyed %s", reflected.Type(), target)
		}
		typeExpression, err := imports.typeExpression(target)
		if err != nil {
			return "", err
		}
		if reflected.IsNil() {
			return typeExpression + "(nil)", nil
		}
		keys := reflected.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		var result strings.Builder
		result.WriteString(typeExpression)
		result.WriteByte('{')
		for _, key := range keys {
			literal, literalErr := documentationExampleLiteral(reflected.MapIndex(key).Interface(), target.Elem(), imports)
			if literalErr != nil {
				return "", literalErr
			}
			fmt.Fprintf(&result, "%q:%s,", key.String(), literal)
		}
		result.WriteByte('}')
		return result.String(), nil
	default:
		literal, err := anyLiteral(reflected.Interface())
		if err != nil {
			return "", err
		}
		if target.Name() != "" && target.PkgPath() != "" {
			typeExpression, typeErr := imports.typeExpression(target)
			if typeErr != nil {
				return "", typeErr
			}
			return typeExpression + "(" + literal + ")", nil
		}
		return literal, nil
	}
}

func writeObjectRules(source *bytes.Buffer, rules []fb.ObjectRule) error {
	fmt.Fprintf(source, "[]fb.ObjectRule{")
	for _, rule := range rules {
		var constructor string
		switch rule.Kind() {
		case fb.ObjectRuleAtLeastOne:
			constructor = "AtLeastOne"
		case fb.ObjectRuleAllOrNone:
			constructor = "AllOrNone"
		case fb.ObjectRuleSameNullState:
			constructor = "SameNullState"
		default:
			return fmt.Errorf("unsupported kind %q", rule.Kind())
		}
		fmt.Fprintf(source, "fb.%s(", constructor)
		for index, field := range rule.Fields() {
			if index > 0 {
				fmt.Fprintf(source, ", ")
			}
			fmt.Fprintf(source, "%q", field)
		}
		fmt.Fprintf(source, "),")
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
	if input.Nullable {
		fmt.Fprintf(source, ", Nullable: true")
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
	if input.MinItems != nil {
		fmt.Fprintf(source, ", MinItems: func() *uint64 { value := uint64(%d); return &value }()", *input.MinItems)
	}
	if input.MaxItems != nil {
		fmt.Fprintf(source, ", MaxItems: func() *uint64 { value := uint64(%d); return &value }()", *input.MaxItems)
	}
	if input.UniqueItems {
		fmt.Fprintf(source, ", UniqueItems: true")
	}
	if input.Items != nil {
		fmt.Fprintf(source, ", Items: &fb.CollectionItemContract{Input: ")
		if err := writeInputContract(source, imports, input.Items.Input, true); err != nil {
			return err
		}
		if input.Items.Documentation != nil {
			fmt.Fprintf(source, ", Documentation: ")
			if err := writeBindingDocumentation(source, imports, input.Items.Documentation, input.Items.Input.GoType); err != nil {
				return err
			}
		}
		if input.Items.Bindings != nil {
			fmt.Fprintf(source, ", Bindings: ")
			if err := writeResponseBindings(source, imports, input.Items.Bindings); err != nil {
				return err
			}
		}
		fmt.Fprintf(source, "}")
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
	if target.Kind() == reflect.Pointer {
		return documentationExampleLiteral(value, target, imports)
	}
	valueType := target
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if !fbContractLiteralKind(valueType.Kind()) {
		return documentationExampleLiteral(value, target, imports)
	}
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

func fbContractLiteralKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func writeOperationDocumentation(source *bytes.Buffer, imports *importSet, documentation fb.OperationDocumentation) error {
	fmt.Fprintf(source, "fb.OperationDocumentation{")
	if documentation.Route != "" {
		fmt.Fprintf(source, "Route: %q, ", documentation.Route)
	}
	fmt.Fprintf(source, "Method: %q, SuccessStatus: %d, OperationID: %q, Summary: %q, Description: %q", documentation.Method, documentation.SuccessStatus, documentation.OperationID, documentation.Summary, documentation.Description)
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
			if response.GoType != nil {
				expression, err := imports.typeExpression(response.GoType)
				if err != nil {
					return err
				}
				fmt.Fprintf(source, ", GoType: reflect.TypeFor[%s]()", expression)
			}
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
