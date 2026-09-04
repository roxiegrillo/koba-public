package openapi

import (
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
	if documentation.Public && documentation.Security != nil {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and Security", description.MethodName)
	}
	if documentation.Public && len(requiredScopes) > 0 {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and RequiredScopes", description.MethodName)
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
		Description: strings.TrimSpace(documentation.Description),
		Tags:        tags,
		Deprecated:  documentation.Deprecated,
		Responses:   openapi3.NewResponses(),
	}
	if len(requiredScopes) > 0 {
		operation.Extensions = map[string]any{
			"x-required-scopes": append([]string(nil), requiredScopes...),
		}
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
	if len(description.RequestBindings) != 0 {
		body, exists := description.BodyBinding()
		if exists {
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
			body, _ := description.BodyBinding()
			schema, err = requestBodyContractSchema(schema, body, description.RequestBindings, schemas)
			if err != nil {
				return nil, fmt.Errorf("koba/openapi: %s request contract: %w", description.MethodName, err)
			}
			if hasContractSchemaConstraints(body.Input) {
				schema = contractSchema(schema, body.Input)
			}
		}
		operation.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().
			WithRequired(requestBodyRequired).
			WithDescription(strings.TrimSpace(documentation.RequestBodyDescription)).
			WithJSONSchemaRef(schema)}
	}

	success := openapi3.NewResponse().WithDescription("Successful response")
	if description.OutputType != nil {
		schema, err := responseSchemaForType(generator, schemas, description.OutputType)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s response schema: %w", description.MethodName, err)
		}
		success.WithJSONSchemaRef(schema)
	}
	successStatus := description.ResolvedSuccessStatus()
	operation.AddResponse(successStatus, success)
	if description.InputType != nil || len(description.Contract.Inputs) != 0 || len(description.RequestBindings) != 0 {
		operation.AddResponse(http.StatusBadRequest, errorResponse("Invalid request"))
	}
	operation.AddResponse(http.StatusInternalServerError, errorResponse("Internal server error"))
	if len(requiredScopes) > 0 {
		operation.AddResponse(http.StatusUnauthorized, errorResponse("Authentication required"))
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
			renderResponseHeaders(response, responseDocumentation.Headers)
			operation.AddResponse(status, response)
		}
	}

	if documentation.Public {
		empty := openapi3.SecurityRequirements{}
		operation.Security = &empty
	} else if len(requiredScopes) > 0 {
		effectiveSecurity := documentation.Security
		if effectiveSecurity == nil {
			effectiveSecurity = config.DefaultSecurity
		}
		if len(effectiveSecurity) == 0 {
			return nil, fmt.Errorf("koba/openapi: %s requires scopes but has no effective security requirement", description.MethodName)
		}
		for index, requirement := range effectiveSecurity {
			if len(requirement) == 0 {
				return nil, fmt.Errorf("koba/openapi: %s security requirement %d allows anonymous access but RequiredScopes is nonempty", description.MethodName, index+1)
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
		if documented, exists := documentation[key]; exists {
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
	if len(values) == 0 {
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
	return root, nil
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
			current.Properties[jsonName] = contractSchema(property, binding.Input)
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
	response.Headers = make(openapi3.Headers, len(documentation))
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
		schema = contractSchema(schema, *input)
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
	return ref
}

func hasContractSchemaConstraints(input fb.InputContract) bool {
	return input.Format != "" ||
		input.Pattern != "" ||
		input.Enum != nil ||
		input.HasDefault ||
		input.Minimum != nil ||
		input.Maximum != nil ||
		input.MinLength != nil ||
		input.MaxLength != nil
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
	if reflected.Type().PkgPath() == "" {
		return reflected.Interface()
	}
	switch reflected.Kind() {
	case reflect.String:
		return reflected.String()
	case reflect.Bool:
		return reflected.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return reflected.Uint()
	case reflect.Float32, reflect.Float64:
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
