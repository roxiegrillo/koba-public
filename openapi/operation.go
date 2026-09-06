package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	fb "github.com/roxiegrillo/koba/v2"
)

func renderOperation(
	generator *openapi3gen.Generator,
	schemas openapi3.Schemas,
	description fb.OperationDescription,
	api fb.APIDescription,
	config Config,
	knownSecurity map[string]struct{},
	openAPIPath string,
) (*openapi3.Operation, error) {
	documentation := description.Documentation
	requiredScopes := description.ResolvedRequiredScopes()
	requiresAuthentication := description.RequiresAuthentication()
	if documentation.Public && documentation.Security != nil {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and Security", description.MethodName)
	}
	if documentation.Public && len(requiredScopes) > 0 {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and RequiredScopes", description.MethodName)
	}
	if documentation.Public && description.HasSubjectBinding() {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and an authenticated subject binding", description.MethodName)
	}
	tags := append([]string(nil), documentation.Tags...)
	if len(tags) == 0 && strings.TrimSpace(api.Name) != "" {
		tags = []string{strings.TrimSpace(api.Name)}
	}
	if len(tags) == 0 {
		tags = []string{defaultTag(openAPIPath)}
	}
	operation := &openapi3.Operation{
		OperationID: description.OperationID,
		Summary:     strings.TrimSpace(documentation.Summary),
		Description: operationDescription(documentation.Description, requiredScopes),
		Tags:        tags,
		Deprecated:  documentation.Deprecated,
		Responses:   openapi3.NewResponses(),
	}
	if len(requiredScopes) > 0 {
		operation.Extensions = map[string]any{
			"x-required-scopes": append([]string(nil), requiredScopes...),
		}
	}
	if description.HasWebSocketTransport() {
		extension, err := renderWebSocketExtension(
			generator,
			schemas,
			config.SchemaCustomizer,
			*description.Contract.Transport.WebSocket,
			description.OperationID,
		)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s WebSocket contract: %w", description.MethodName, err)
		}
		if operation.Extensions == nil {
			operation.Extensions = make(map[string]any)
		}
		operation.Extensions["x-websocket"] = extension
	}
	if operation.Summary == "" {
		operation.Summary = splitIdentifier(description.MethodName)
	}

	parameters, err := renderOperationParameters(description, schemas, config.SchemaCustomizer)
	if err != nil {
		return nil, err
	}
	operation.Parameters = parameters
	if err := validatePathParameters(description.MethodName, openAPIPath, operation.Parameters); err != nil {
		return nil, err
	}

	requestBodyType := description.InputType
	requestBodyRequired := true
	var requestBodyBinding fb.RequestBinding
	hasRequestBodyBinding := false
	if len(description.RequestBindings) != 0 {
		body, exists := description.BodyBinding()
		if exists {
			requestBodyBinding = body
			hasRequestBodyBinding = true
			requestBodyType = body.Input.GoType
			requestBodyRequired = body.Input.Required
		} else {
			requestBodyType = nil
		}
	}
	if requestBodyType != nil {
		schema, err := schemaForType(generator, schemas, requestBodyType)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s request schema: %w", description.MethodName, err)
		}
		if len(description.RequestBindings) != 0 {
			schema, err = requestBodyContractSchema(schema, requestBodyBinding, description.RequestBindings, schemas)
			if err != nil {
				return nil, fmt.Errorf("koba/openapi: %s request contract: %w", description.MethodName, err)
			}
			schema, err = documentedInputContractSchema(
				schema,
				requestBodyBinding.Input,
				requestBodyBinding.Documentation,
				schemas,
			)
			if err != nil {
				return nil, fmt.Errorf("koba/openapi: %s request body contract: %w", description.MethodName, err)
			}
		}
		requestBodyDescription := strings.TrimSpace(documentation.RequestBodyDescription)
		if hasRequestBodyBinding && requestBodyBinding.Documentation != nil {
			requestBodyDescription = requestBodyBinding.Documentation.Description
		}
		operation.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().
			WithRequired(requestBodyRequired).
			WithDescription(requestBodyDescription).
			WithJSONSchemaRef(schema)}
	}

	success := openapi3.NewResponse().WithDescription("Successful response")
	if description.HasWebSocketTransport() {
		success = webSocketUpgradeResponse()
	} else if description.OutputType != nil {
		schema, err := responseSchemaForType(generator, schemas, description.OutputType)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s response schema: %w", description.MethodName, err)
		}
		if len(description.ResponseBindings) != 0 {
			schema, err = responseContractSchema(schema, description.OutputType, description.ResponseBindings, schemas)
			if err != nil {
				return nil, fmt.Errorf("koba/openapi: %s response contract: %w", description.MethodName, err)
			}
		}
		success.WithJSONSchemaRef(schema)
	}
	successStatus := description.ResolvedSuccessStatus()
	operation.AddResponse(successStatus, success)
	hasInvalidClientRequest := description.InputType != nil || len(description.Contract.Inputs) != 0
	if len(description.RequestBindings) != 0 {
		hasInvalidClientRequest = description.HasClientRequestBinding()
	}
	if hasInvalidClientRequest {
		operation.AddResponse(http.StatusBadRequest, errorResponse("Invalid request"))
	}
	operation.AddResponse(http.StatusInternalServerError, errorResponse("Internal server error"))
	if requiresAuthentication {
		operation.AddResponse(http.StatusUnauthorized, errorResponse("Authentication required"))
	}
	if len(requiredScopes) > 0 {
		operation.AddResponse(http.StatusForbidden, errorResponse("Required OAuth scope not granted"))
	}
	statusCodes := make([]int, 0, len(documentation.Responses))
	for status := range documentation.Responses {
		statusCodes = append(statusCodes, status)
	}
	sort.Ints(statusCodes)
	for _, status := range statusCodes {
		responseDocumentation := documentation.Responses[status]
		if status != successStatus && (status < 400 || status > 599) {
			return nil, fmt.Errorf(
				"koba/openapi: %s documents HTTP %d, but its resolved success status is %d",
				description.MethodName,
				status,
				successStatus,
			)
		}
		responseDescription := strings.TrimSpace(responseDocumentation.Description)
		if responseDescription == "" {
			responseDescription = http.StatusText(status)
		}
		if status == successStatus {
			// A 200 metadata entry customizes the inferred success description;
			// it must not discard the reflected response schema.
			existing := operation.Responses.Value(strconv.Itoa(successStatus))
			if existing == nil || existing.Value == nil {
				operation.AddResponse(status, openapi3.NewResponse().WithDescription(responseDescription))
			} else {
				existing.Value.Description = &responseDescription
			}
			existing = operation.Responses.Value(strconv.Itoa(successStatus))
			if existing != nil && existing.Value != nil {
				renderResponseHeaders(existing.Value, responseDocumentation.Headers)
			}
		} else {
			response := errorResponse(responseDescription)
			if responseDocumentation.GoType != nil {
				schema, err := responseSchemaForType(generator, schemas, responseDocumentation.GoType)
				if err != nil {
					return nil, fmt.Errorf(
						"koba/openapi: %s response HTTP %d schema: %w",
						description.MethodName,
						status,
						err,
					)
				}
				response = openapi3.NewResponse().WithDescription(responseDescription).WithJSONSchemaRef(schema)
			}
			renderResponseHeaders(response, responseDocumentation.Headers)
			operation.AddResponse(status, response)
		}
	}
	if description.HasWebSocketTransport() {
		response := operation.Responses.Value(strconv.Itoa(http.StatusSwitchingProtocols))
		if response == nil || response.Value == nil {
			return nil, fmt.Errorf("koba/openapi: %s WebSocket response disappeared", description.MethodName)
		}
		setWebSocketUpgradeHeaders(response.Value)
	}

	if documentation.Public {
		empty := openapi3.SecurityRequirements{}
		operation.Security = &empty
	} else if requiresAuthentication {
		effectiveSecurity := documentation.Security
		if effectiveSecurity == nil {
			effectiveSecurity = config.DefaultSecurity
		}
		if len(effectiveSecurity) == 0 {
			return nil, fmt.Errorf("koba/openapi: %s requires authentication but has no effective security requirement", description.MethodName)
		}
		for index, requirement := range effectiveSecurity {
			if len(requirement) == 0 {
				return nil, fmt.Errorf("koba/openapi: %s security requirement %d allows anonymous access but authentication is required", description.MethodName, index+1)
			}
		}
		requirements, err := renderSecurityRequirements(effectiveSecurity, knownSecurity)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s: %w", description.MethodName, err)
		}
		operation.Security = &requirements
	} else if documentation.Security != nil {
		requirements, err := renderSecurityRequirements(documentation.Security, knownSecurity)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s: %w", description.MethodName, err)
		}
		operation.Security = &requirements
	} else if len(config.DefaultSecurity) == 0 {
		// Avoid emitting a redundant empty operation-level security value.
		operation.Security = nil
	}
	return operation, nil
}

func operationDescription(description string, requiredScopes []string) string {
	description = strings.TrimSpace(description)
	if len(requiredScopes) == 0 {
		return description
	}
	scopes := "Required OAuth scopes: " + strings.Join(requiredScopes, ", ") + "."
	if description == "" {
		return scopes
	}
	return description + "\n\n" + scopes
}

func renderOperationParameters(
	description fb.OperationDescription,
	schemas openapi3.Schemas,
	customizer SchemaCustomizer,
) (openapi3.Parameters, error) {
	if len(description.RequestBindings) == 0 {
		return renderLegacyParameters(description, schemas, customizer)
	}

	documentation := make(map[string]fb.ParameterDocumentation, len(description.Documentation.Parameters))
	for _, parameter := range description.Documentation.Parameters {
		documentation[operationParameterKey(parameter.In, parameter.Name)] = parameter
	}

	parameters := make(openapi3.Parameters, 0, len(description.RequestBindings))
	seen := make(map[string]struct{}, len(description.RequestBindings))
	for _, binding := range description.RequestBindings {
		location, isParameter := bindingParameterLocation(binding.Source)
		if !isParameter {
			continue
		}
		key := operationParameterKey(location, binding.Name)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("koba/openapi: %s has duplicate parameter %s", description.MethodName, key)
		}
		seen[key] = struct{}{}

		parameterDescription := fb.ParameterDocumentation{Name: binding.Name, In: location}
		if binding.Documentation != nil {
			parameterDescription.Description = binding.Documentation.Description
			parameterDescription.Deprecated = binding.Documentation.Deprecated
			parameterDescription.Example = binding.Documentation.Example
		} else if documented, exists := documentation[key]; exists {
			parameterDescription = documented
			// The request binding owns transport identity and all structural
			// metadata. ParameterDocumentation only enriches it with prose and an
			// example, and therefore does not need a ContractInput indirection.
			parameterDescription.Name = binding.Name
			parameterDescription.In = location
		}
		input := binding.Input
		parameter, err := renderParameter(parameterDescription, &input, schemas, customizer)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s parameter %s: %w", description.MethodName, binding.Name, err)
		}
		parameters = append(parameters, &openapi3.ParameterRef{Value: parameter})
	}
	return parameters, nil
}

func renderLegacyParameters(
	description fb.OperationDescription,
	schemas openapi3.Schemas,
	customizer SchemaCustomizer,
) (openapi3.Parameters, error) {
	parameters := make(openapi3.Parameters, 0, len(description.Documentation.Parameters))
	seen := make(map[string]struct{}, len(description.Documentation.Parameters))
	for _, parameterDescription := range description.Documentation.Parameters {
		key := operationParameterKey(parameterDescription.In, parameterDescription.Name)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("koba/openapi: %s has duplicate parameter %s", description.MethodName, key)
		}
		seen[key] = struct{}{}
		var inputContract *fb.InputContract
		if parameterDescription.ContractInput != "" {
			input := description.Contract.Inputs[parameterDescription.ContractInput]
			inputContract = &input
		}
		parameter, err := renderParameter(parameterDescription, inputContract, schemas, customizer)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s parameter %s: %w", description.MethodName, parameterDescription.Name, err)
		}
		parameters = append(parameters, &openapi3.ParameterRef{Value: parameter})
	}
	return parameters, nil
}

func operationParameterKey(location fb.ParameterLocation, name string) string {
	if location == fb.ParameterInHeader {
		name = strings.ToLower(name)
	}
	return string(location) + ":" + name
}

func bindingParameterLocation(source fb.BindingSource) (fb.ParameterLocation, bool) {
	switch source {
	case fb.BindingSourceQuery:
		return fb.ParameterInQuery, true
	case fb.BindingSourcePath:
		return fb.ParameterInPath, true
	case fb.BindingSourceHeader:
		return fb.ParameterInHeader, true
	case fb.BindingSourceCookie:
		return fb.ParameterInCookie, true
	default:
		return "", false
	}
}

// requestBodyContractSchema projects BodyObject value contracts onto a
// request-local copy of the reflected body schema. Copy-on-write is important:
// the same Go component may be used by operations with different contracts.
func requestBodyContractSchema(
	ref *openapi3.SchemaRef,
	body fb.RequestBinding,
	bindings []fb.RequestBinding,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	values := make([]fb.RequestBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Source == fb.BindingSourceValue {
			values = append(values, binding)
		}
	}
	if len(values) == 0 && len(body.ObjectRules) == 0 {
		return ref, nil
	}

	root, err := mutableSchemaRef(ref, schemas)
	if err != nil {
		return nil, err
	}
	for _, binding := range values {
		if err := applyBodyValueContract(root, body, binding, schemas); err != nil {
			return nil, err
		}
	}
	if err := applyBodyObjectRules(root, body.Input.GoType, body.ObjectRules, schemas); err != nil {
		return nil, err
	}
	return root, nil
}

func applyBodyObjectRules(
	root *openapi3.SchemaRef,
	bodyType reflect.Type,
	rules []fb.ObjectRule,
	schemas openapi3.Schemas,
) error {
	if len(rules) == 0 {
		return nil
	}
	if root == nil || root.Value == nil {
		return fmt.Errorf("body object rules require a resolved object schema")
	}
	for _, rule := range rules {
		presence := make(openapi3.SchemaRefs, 0, len(rule.Fields()))
		for _, field := range rule.Fields() {
			schema, err := objectFieldPresenceSchema(bodyType, field)
			if err != nil {
				return err
			}
			presence = append(presence, schema)
		}

		constraint := openapi3.NewObjectSchema()
		switch rule.Kind() {
		case fb.ObjectRuleAtLeastOne:
			constraint.AnyOf = presence
		case fb.ObjectRuleAllOrNone:
			all := openapi3.NewObjectSchema()
			all.AllOf = presence
			none := openapi3.NewObjectSchema()
			none.Not = &openapi3.SchemaRef{Value: &openapi3.Schema{AnyOf: presence}}
			constraint.OneOf = openapi3.SchemaRefs{
				{Value: all},
				{Value: none},
			}
		case fb.ObjectRuleSameNullState:
			nullValues := make(openapi3.SchemaRefs, 0, len(rule.Fields()))
			nonNullValues := make(openapi3.SchemaRefs, 0, len(rule.Fields()))
			for _, field := range rule.Fields() {
				nullValue, err := objectFieldStateSchema(root, bodyType, field, true, schemas)
				if err != nil {
					return err
				}
				nonNullValue, err := objectFieldStateSchema(root, bodyType, field, false, schemas)
				if err != nil {
					return err
				}
				nullValues = append(nullValues, nullValue)
				nonNullValues = append(nonNullValues, nonNullValue)
			}
			// Absent fields are ignored. A valid object therefore has either no
			// present null values or no present non-null values. anyOf is
			// intentional because an object with every field absent satisfies both.
			noNullValues := openapi3.NewObjectSchema()
			noNullValues.Not = &openapi3.SchemaRef{Value: &openapi3.Schema{AnyOf: nullValues}}
			noNonNullValues := openapi3.NewObjectSchema()
			noNonNullValues.Not = &openapi3.SchemaRef{Value: &openapi3.Schema{AnyOf: nonNullValues}}
			constraint.AnyOf = openapi3.SchemaRefs{
				{Value: noNullValues},
				{Value: noNonNullValues},
			}
		default:
			return fmt.Errorf("unsupported body object rule kind %q", rule.Kind())
		}
		root.Value.AllOf = append(root.Value.AllOf, &openapi3.SchemaRef{Value: constraint})
	}
	return nil
}

func objectFieldStateSchema(
	root *openapi3.SchemaRef,
	bodyType reflect.Type,
	fieldPath string,
	null bool,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	jsonPath, err := objectFieldJSONPath(bodyType, fieldPath)
	if err != nil {
		return nil, err
	}
	property, err := objectFieldSchema(root, jsonPath, schemas)
	if err != nil {
		return nil, fmt.Errorf("object rule field %q: %w", fieldPath, err)
	}
	leaf, err := mutableSchemaRef(property, schemas)
	if err != nil {
		return nil, err
	}
	leaf.Value.Nullable = null
	if null {
		leaf.Value.Enum = []any{nil}
	}
	return objectFieldConstraintSchema(bodyType, fieldPath, leaf)
}

func objectFieldPresenceSchema(bodyType reflect.Type, fieldPath string) (*openapi3.SchemaRef, error) {
	return objectFieldConstraintSchema(bodyType, fieldPath, nil)
}

func objectFieldConstraintSchema(
	bodyType reflect.Type,
	fieldPath string,
	leaf *openapi3.SchemaRef,
) (*openapi3.SchemaRef, error) {
	jsonPath, err := objectFieldJSONPath(bodyType, fieldPath)
	if err != nil {
		return nil, err
	}

	child := leaf
	for index := len(jsonPath) - 1; index >= 0; index-- {
		schema := openapi3.NewObjectSchema()
		schema.Required = []string{jsonPath[index]}
		if child != nil {
			schema.Properties = openapi3.Schemas{jsonPath[index]: child}
		}
		child = &openapi3.SchemaRef{Value: schema}
	}
	return child, nil
}

func objectFieldJSONPath(bodyType reflect.Type, fieldPath string) ([]string, error) {
	current := bodyType
	jsonPath := make([]string, 0, strings.Count(fieldPath, ".")+1)
	for _, fieldName := range strings.Split(fieldPath, ".") {
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return nil, fmt.Errorf("object rule field %q traverses non-struct type %s", fieldPath, current)
		}
		field, exists := directOpenAPIField(current, fieldName)
		if !exists {
			return nil, fmt.Errorf("object rule field %q does not exist in %s", fieldPath, current)
		}
		jsonName, included := openAPIJSONFieldName(field)
		if !included {
			return nil, fmt.Errorf("object rule field %q is excluded from JSON", fieldPath)
		}
		jsonPath = append(jsonPath, jsonName)
		current = field.Type
	}
	return jsonPath, nil
}

func objectFieldSchema(
	root *openapi3.SchemaRef,
	jsonPath []string,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	current := root
	for _, jsonName := range jsonPath {
		resolved := resolvedSchema(current, schemas)
		if resolved == nil {
			return nil, fmt.Errorf("has no resolved schema")
		}
		property := resolved.Properties[jsonName]
		if property == nil {
			return nil, fmt.Errorf("property %q has no schema", jsonName)
		}
		current = property
	}
	return current, nil
}

func applyBodyValueContract(
	root *openapi3.SchemaRef,
	body fb.RequestBinding,
	binding fb.RequestBinding,
	schemas openapi3.Schemas,
) error {
	if len(binding.FieldPath) <= len(body.FieldPath) {
		return fmt.Errorf("body value %s does not identify a descendant field", strings.Join(binding.FieldPath, "."))
	}
	for index := range body.FieldPath {
		if body.FieldPath[index] != binding.FieldPath[index] {
			return fmt.Errorf("body value %s is outside body field %s", strings.Join(binding.FieldPath, "."), strings.Join(body.FieldPath, "."))
		}
	}

	currentRef := root
	currentType := body.Input.GoType
	relative := binding.FieldPath[len(body.FieldPath):]
	for index, fieldName := range relative {
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
		if currentType.Kind() != reflect.Struct {
			return fmt.Errorf("body value %s traverses non-struct type %s", strings.Join(binding.FieldPath, "."), currentType)
		}
		field, exists := directOpenAPIField(currentType, fieldName)
		if !exists {
			return fmt.Errorf("body value %s references missing field %s", strings.Join(binding.FieldPath, "."), fieldName)
		}
		jsonName, included := openAPIJSONFieldName(field)
		if !included {
			return fmt.Errorf("body value %s references JSON-excluded field %s", strings.Join(binding.FieldPath, "."), fieldName)
		}

		current := resolvedSchema(currentRef, schemas)
		if current == nil {
			return fmt.Errorf("body value %s has an unresolved parent schema", strings.Join(binding.FieldPath, "."))
		}
		property := current.Properties[jsonName]
		if property == nil {
			return fmt.Errorf("body value %s has no schema property %q", strings.Join(binding.FieldPath, "."), jsonName)
		}
		// A required leaf cannot exist unless every JSON object on its path also
		// exists. Mirror runtime presence validation by making the complete path
		// required in the operation-local schema.
		if binding.Input.Required {
			current.Required = appendUnique(current.Required, jsonName)
			sort.Strings(current.Required)
		}
		if index == len(relative)-1 {
			contracted, contractErr := documentedInputContractSchema(
				property,
				binding.Input,
				binding.Documentation,
				schemas,
			)
			if contractErr != nil {
				return fmt.Errorf("body value %s: %w", strings.Join(binding.FieldPath, "."), contractErr)
			}
			current.Properties[jsonName] = contracted
			return nil
		}

		child, err := mutableSchemaRef(property, schemas)
		if err != nil {
			return fmt.Errorf("body value %s: field %s: %w", strings.Join(binding.FieldPath, "."), fieldName, err)
		}
		current.Properties[jsonName] = child
		currentRef = child
		currentType = field.Type
	}
	return nil
}

func mutableSchemaRef(ref *openapi3.SchemaRef, schemas openapi3.Schemas) (*openapi3.SchemaRef, error) {
	value := resolvedSchema(ref, schemas)
	if value == nil {
		return nil, fmt.Errorf("cannot resolve schema %q", ref.Ref)
	}
	clone := *value
	clone.Required = append([]string(nil), value.Required...)
	clone.Properties = make(openapi3.Schemas, len(value.Properties))
	for name, property := range value.Properties {
		clone.Properties[name] = property
	}
	return &openapi3.SchemaRef{Value: &clone}, nil
}

func directOpenAPIField(structType reflect.Type, name string) (reflect.StructField, bool) {
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		if field.Name == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func openAPIJSONFieldName(field reflect.StructField) (string, bool) {
	name := strings.Split(field.Tag.Get("json"), ",")[0]
	if name == "-" {
		return "", false
	}
	if name == "" {
		name = field.Name
	}
	return name, true
}

func renderResponseHeaders(response *openapi3.Response, documentation map[string]fb.HeaderDocumentation) {
	if documentation == nil {
		return
	}
	if response.Headers == nil {
		response.Headers = make(openapi3.Headers, len(documentation))
	}
	for name, description := range documentation {
		header := &openapi3.Header{}
		header.Description = strings.TrimSpace(description.Description)
		header.Required = description.Required
		schema := openapi3.NewStringSchema()
		schema.Format = strings.TrimSpace(description.Format)
		schema.Pattern = description.Pattern
		if description.Enum != nil {
			schema.Enum = make([]any, len(description.Enum))
			for index, value := range description.Enum {
				schema.Enum[index] = value
			}
		}
		header.Schema = &openapi3.SchemaRef{Value: schema}
		if description.Example != "" {
			header.Example = description.Example
		}
		response.Headers[name] = &openapi3.HeaderRef{Value: header}
	}
}

func renderParameter(
	description fb.ParameterDocumentation,
	input *fb.InputContract,
	schemas openapi3.Schemas,
	customizer SchemaCustomizer,
) (*openapi3.Parameter, error) {
	var parameter *openapi3.Parameter
	switch description.In {
	case fb.ParameterInQuery:
		parameter = openapi3.NewQueryParameter(description.Name)
	case fb.ParameterInHeader:
		parameter = openapi3.NewHeaderParameter(description.Name)
	case fb.ParameterInCookie:
		parameter = openapi3.NewCookieParameter(description.Name)
	case fb.ParameterInPath:
		parameter = openapi3.NewPathParameter(description.Name)
	default:
		return nil, fmt.Errorf("unsupported location %q", description.In)
	}
	goType := description.GoType
	if input != nil {
		goType = input.GoType
	}
	generator := newSchemaGenerator(schemas, customizer)
	schema, err := schemaForType(generator, schemas, goType)
	if err != nil {
		return nil, err
	}
	if input == nil {
		schema = constrainedSchema(schema, description)
	} else {
		schema, err = documentedInputContractSchema(schema, *input, nil, schemas)
		if err != nil {
			return nil, err
		}
	}
	parameter.Description = strings.TrimSpace(description.Description)
	parameter.Required = description.Required || description.In == fb.ParameterInPath
	if input != nil {
		parameter.Required = input.Required || description.In == fb.ParameterInPath
	}
	parameter.Deprecated = description.Deprecated
	parameter.Schema = schema
	if description.Example != nil {
		parameter.Example = openAPIScalarValue(description.Example)
	}
	return parameter, nil
}

func contractSchema(ref *openapi3.SchemaRef, input fb.InputContract) *openapi3.SchemaRef {
	ref = independentParameterSchema(ref)
	ref.Value.Nullable = input.Nullable
	if input.Format != "" {
		ref.Value.Format = input.Format
	}
	if input.Pattern != "" {
		ref.Value.Pattern = input.Pattern
	}
	if input.Enum != nil {
		ref.Value.Enum = make([]any, len(input.Enum))
		for index, value := range input.Enum {
			ref.Value.Enum[index] = openAPIScalarValue(value)
		}
	}
	if input.HasDefault {
		ref.Value.Default = openAPIScalarValue(input.Default)
	}
	if input.Minimum != nil {
		value := numericSchemaValue(input.Minimum)
		ref.Value.Min = &value
	}
	if input.Maximum != nil {
		value := numericSchemaValue(input.Maximum)
		ref.Value.Max = &value
	}
	if input.MinLength != nil {
		ref.Value.MinLength = *input.MinLength
	}
	if input.MaxLength != nil {
		value := *input.MaxLength
		ref.Value.MaxLength = &value
	}
	if input.MinItems != nil {
		ref.Value.MinItems = *input.MinItems
	}
	if input.MaxItems != nil {
		value := *input.MaxItems
		ref.Value.MaxItems = &value
	}
	if input.UniqueItems {
		ref.Value.UniqueItems = true
	}
	return ref
}

func documentedContractSchema(
	ref *openapi3.SchemaRef,
	input fb.InputContract,
	documentation *fb.BindingDocumentation,
) *openapi3.SchemaRef {
	ref = contractSchema(ref, input)
	if documentation == nil {
		return ref
	}
	ref.Value.Description = strings.TrimSpace(documentation.Description)
	ref.Value.Deprecated = documentation.Deprecated
	if documentation.Example != nil {
		ref.Value.Example = openAPIScalarValue(documentation.Example)
	}
	return ref
}

// documentedInputContractSchema projects a complete structural contract onto
// a schema-local copy. Collection element descriptors are recursively applied
// without mutating shared component schemas.
func documentedInputContractSchema(
	ref *openapi3.SchemaRef,
	input fb.InputContract,
	documentation *fb.BindingDocumentation,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	ref = documentedContractSchema(ref, input, documentation)
	if input.Items == nil {
		return ref, nil
	}
	root, err := mutableSchemaRef(ref, schemas)
	if err != nil {
		return nil, err
	}
	if root.Value.Items == nil {
		return nil, fmt.Errorf("collection contract for %s has no item schema", input.GoType)
	}
	item, err := mutableSchemaRef(root.Value.Items, schemas)
	if err != nil {
		return nil, fmt.Errorf("collection item: %w", err)
	}
	item, err = documentedInputContractSchema(
		item,
		input.Items.Input,
		input.Items.Documentation,
		schemas,
	)
	if err != nil {
		return nil, fmt.Errorf("collection item: %w", err)
	}
	for _, binding := range input.Items.Bindings {
		if err := applyResponseValueContract(item, input.Items.Input.GoType, binding, schemas); err != nil {
			return nil, fmt.Errorf("collection item field: %w", err)
		}
	}
	root.Value.Items = item
	return root, nil
}

func openAPIScalarValue(value any) any {
	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return nil
	}
	switch reflected.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
		// OpenAPI values must use JSON-compatible maps and arrays. Returning a
		// Go map such as map[string]string is rejected by kin-openapi validation
		// and does not model the JSON value seen by a transport.
		encoded, err := json.Marshal(reflected.Interface())
		if err != nil {
			return value
		}
		var normalized any
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&normalized); err != nil {
			return value
		}
		return normalized
	case reflect.String:
		if reflected.Type().PkgPath() == "" {
			return reflected.Interface()
		}
		return reflected.String()
	case reflect.Bool:
		if reflected.Type().PkgPath() == "" {
			return reflected.Interface()
		}
		return reflected.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if reflected.Type().PkgPath() == "" {
			return reflected.Interface()
		}
		return reflected.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if reflected.Type().PkgPath() == "" {
			return reflected.Interface()
		}
		return reflected.Uint()
	case reflect.Float32, reflect.Float64:
		if reflected.Type().PkgPath() == "" {
			return reflected.Interface()
		}
		return reflected.Float()
	default:
		return value
	}
}

func independentParameterSchema(ref *openapi3.SchemaRef) *openapi3.SchemaRef {
	if ref.Value == nil {
		return &openapi3.SchemaRef{Value: &openapi3.Schema{AllOf: openapi3.SchemaRefs{ref}}}
	}
	value := *ref.Value
	value.Enum = append([]any(nil), ref.Value.Enum...)
	return &openapi3.SchemaRef{Value: &value}
}

func numericSchemaValue(value any) float64 {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(reflected.Uint())
	case reflect.Float32, reflect.Float64:
		return reflected.Float()
	default:
		return 0
	}
}

func constrainedSchema(ref *openapi3.SchemaRef, description fb.ParameterDocumentation) *openapi3.SchemaRef {
	if description.Format == "" && description.Pattern == "" && len(description.Enum) == 0 {
		return ref
	}
	if ref.Value == nil {
		ref = &openapi3.SchemaRef{Value: &openapi3.Schema{AllOf: openapi3.SchemaRefs{ref}}}
	}
	ref.Value.Format = description.Format
	ref.Value.Pattern = description.Pattern
	ref.Value.Enum = append([]any(nil), description.Enum...)
	return ref
}
