// Package openapi renders koba's neutral API descriptions as an
// OpenAPI document. Application APIs remain free of OpenAPI-specific types.
package openapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	fb "github.com/roxiegrillo/koba/v2"
	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

const Version = "3.0.3"

// Server describes one public base URL for an API.
type Server struct {
	URL         string
	Description string
}

// SecurityScheme describes a document-level authentication mechanism.
// ValuePrefix is emitted as x-auth-value-prefix for generators which support
// API-key values such as "Token <value>".
type SecurityScheme struct {
	Name         string
	Type         string
	Description  string
	In           string
	Parameter    string
	Scheme       string
	BearerFormat string
	ValuePrefix  string
}

const (
	SecurityTypeAPIKey = "apiKey"
	SecurityTypeHTTP   = "http"
)

// SecurityRequirement aliases the transport-neutral contract type for
// convenient use in OpenAPI configuration.
type SecurityRequirement = fb.SecurityRequirement

// SchemaContext is passed to a project's optional schema customizer. It
// provides the Go type and struct tag which produced an OpenAPI schema while
// keeping customization at the adapter boundary.
type SchemaContext struct {
	Name      string
	GoType    reflect.Type
	StructTag reflect.StructTag
	Schema    *openapi3.Schema
}

// SchemaCustomizer documents project-specific JSON encodings or validation
// constraints which reflection cannot infer, such as custom marshalers.
type SchemaCustomizer func(SchemaContext) error

// Config contains project-level OpenAPI information. DefaultSecurity defines
// the AND/OR authentication alternatives for every operation unless its
// documentation overrides them or marks the operation Public.
type Config struct {
	Title            string
	Version          string
	Description      string
	Servers          []Server
	SecuritySchemes  []SecurityScheme
	DefaultSecurity  []SecurityRequirement
	ExternalDocsURL  string
	SchemaCustomizer SchemaCustomizer
}

// Generate reflects APIs, merges their optional neutral documentation, and
// returns a validated OpenAPI 3.0.3 document.
func Generate(config Config, apis ...any) (*openapi3.T, error) {
	descriptions := make([]fb.APIDescription, 0, len(apis))
	for index, api := range apis {
		description, err := fb.DescribeAPI(api)
		if err != nil {
			return nil, fmt.Errorf("describe API %d: %w", index+1, err)
		}
		descriptions = append(descriptions, description)
	}
	return Build(config, descriptions)
}

// GenerateFromApp builds a document from the neutral descriptions captured by
// KobaApp.AddApi.
func GenerateFromApp(config Config, app fb.WithAPIDescriptions) (*openapi3.T, error) {
	if app == nil {
		return nil, fmt.Errorf("koba/openapi: app is nil")
	}
	return Build(config, app.RegisteredAPIDescriptions())
}

// Build converts already-resolved neutral descriptions to OpenAPI.
func Build(config Config, descriptions []fb.APIDescription) (*openapi3.T, error) {
	if strings.TrimSpace(config.Title) == "" {
		return nil, fmt.Errorf("koba/openapi: title is required")
	}
	if strings.TrimSpace(config.Version) == "" {
		return nil, fmt.Errorf("koba/openapi: API version is required")
	}

	components := openapi3.NewComponents()
	components.Schemas = make(openapi3.Schemas)
	components.SecuritySchemes = make(openapi3.SecuritySchemes)
	document := &openapi3.T{
		OpenAPI: Version,
		Info: &openapi3.Info{
			Title:       strings.TrimSpace(config.Title),
			Version:     strings.TrimSpace(config.Version),
			Description: strings.TrimSpace(config.Description),
		},
		Paths:      openapi3.NewPaths(),
		Components: &components,
	}
	if strings.TrimSpace(config.ExternalDocsURL) != "" {
		document.ExternalDocs = &openapi3.ExternalDocs{URL: strings.TrimSpace(config.ExternalDocsURL)}
	}
	for _, server := range config.Servers {
		if strings.TrimSpace(server.URL) == "" {
			return nil, fmt.Errorf("koba/openapi: server URL cannot be empty")
		}
		document.Servers = append(document.Servers, &openapi3.Server{
			URL: strings.TrimSpace(server.URL), Description: strings.TrimSpace(server.Description),
		})
	}

	knownSecurity := make(map[string]struct{}, len(config.SecuritySchemes))
	for _, definition := range config.SecuritySchemes {
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			return nil, fmt.Errorf("koba/openapi: security scheme name is required")
		}
		if _, exists := knownSecurity[name]; exists {
			return nil, fmt.Errorf("koba/openapi: duplicate security scheme %q", name)
		}
		knownSecurity[name] = struct{}{}
		scheme, err := renderSecurityScheme(definition)
		if err != nil {
			return nil, err
		}
		components.SecuritySchemes[name] = &openapi3.SecuritySchemeRef{Value: scheme}
	}
	defaultSecurity, err := renderSecurityRequirements(config.DefaultSecurity, knownSecurity)
	if err != nil {
		return nil, err
	}
	document.Security = defaultSecurity

	components.Schemas["ErrorResponse"] = &openapi3.SchemaRef{Value: errorResponseSchema()}
	schemaGenerator := newSchemaGenerator(components.Schemas, config.SchemaCustomizer)
	seenOperations := make(map[string]string)
	seenRoutes := make(map[string]string)
	tagDescriptions := make(map[string]string)

	for _, api := range descriptions {
		// Build historically accepted manually assembled descriptions with
		// lowercase or padded methods. Normalize a private copy before applying
		// the same strict validation used by runtime execution plans.
		api.Operations = append([]fb.OperationDescription(nil), api.Operations...)
		for index := range api.Operations {
			api.Operations[index].HTTPMethod = strings.ToUpper(strings.TrimSpace(api.Operations[index].HTTPMethod))
		}
		if err := fb.ValidateAPIDescription(api); err != nil {
			return nil, fmt.Errorf("koba/openapi: invalid API description: %w", err)
		}
		if name := strings.TrimSpace(api.Name); name != "" {
			tagDescriptions[name] = strings.TrimSpace(api.Description)
		}
		for _, description := range api.Operations {
			httpMethod := description.HTTPMethod
			switch httpMethod {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				return nil, fmt.Errorf("koba/openapi: %s has unsupported HTTP method %q; the runtime supports GET, POST, PUT, PATCH, and DELETE", description.MethodName, description.HTTPMethod)
			}
			description.HTTPMethod = httpMethod
			location := httpMethod + " " + description.Route
			if previous, exists := seenOperations[description.OperationID]; exists {
				return nil, fmt.Errorf("koba/openapi: duplicate operation ID %q on %s and %s", description.OperationID, previous, location)
			}
			seenOperations[description.OperationID] = location

			openAPIPath := normalizePath(description.Route)
			routeKey := strings.ToUpper(description.HTTPMethod) + " " + openAPIPath
			if previous, exists := seenRoutes[routeKey]; exists {
				return nil, fmt.Errorf("koba/openapi: duplicate operation %s from %s and %s", routeKey, previous, description.MethodName)
			}
			seenRoutes[routeKey] = description.MethodName

			operation, err := renderOperation(schemaGenerator, components.Schemas, description, api, config, knownSecurity, openAPIPath)
			if err != nil {
				return nil, err
			}
			for _, tag := range operation.Tags {
				if _, exists := tagDescriptions[tag]; !exists {
					tagDescriptions[tag] = ""
				}
			}
			item := document.Paths.Find(openAPIPath)
			if item == nil {
				item = &openapi3.PathItem{}
				document.Paths.Set(openAPIPath, item)
			}
			item.SetOperation(description.HTTPMethod, operation)
		}
	}

	tagNames := make([]string, 0, len(tagDescriptions))
	for name := range tagDescriptions {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)
	for _, name := range tagNames {
		document.Tags = append(document.Tags, &openapi3.Tag{Name: name, Description: tagDescriptions[name]})
	}

	// openapi3gen deliberately serializes component usages as bare $refs. A
	// programmatically built document therefore needs the same reference
	// resolution pass that openapi3.Loader performs for a document loaded from
	// disk before it can be validated (and before callers can inspect Ref.Value).
	if err := openapi3.NewLoader().ResolveRefsIn(document, nil); err != nil {
		return nil, fmt.Errorf("koba/openapi: resolve generated document references: %w", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("koba/openapi: validate generated document: %w", err)
	}
	return document, nil
}

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
	if documentation.Public && documentation.Security != nil {
		return nil, fmt.Errorf("koba/openapi: %s cannot set both Public and Security", description.MethodName)
	}
	if documentation.Security != nil && len(documentation.Security) == 0 {
		return nil, fmt.Errorf("koba/openapi: %s has an empty Security override; use Public for an unauthenticated operation", description.MethodName)
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
	if operation.Summary == "" {
		operation.Summary = splitIdentifier(description.MethodName)
	}

	seenParameters := make(map[string]struct{}, len(documentation.Parameters))
	for _, parameterDescription := range documentation.Parameters {
		parameterName := parameterDescription.Name
		if parameterDescription.In == fb.ParameterInHeader {
			parameterName = strings.ToLower(parameterName)
		}
		key := string(parameterDescription.In) + ":" + parameterName
		if _, exists := seenParameters[key]; exists {
			return nil, fmt.Errorf("koba/openapi: %s has duplicate parameter %s", description.MethodName, key)
		}
		seenParameters[key] = struct{}{}
		parameter, err := renderParameter(parameterDescription, schemas, config.SchemaCustomizer)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s parameter %s: %w", description.MethodName, parameterDescription.Name, err)
		}
		operation.Parameters = append(operation.Parameters, &openapi3.ParameterRef{Value: parameter})
	}
	if err := validatePathParameters(description.MethodName, openAPIPath, operation.Parameters); err != nil {
		return nil, err
	}

	if description.InputType != nil {
		schema, err := schemaForType(generator, schemas, description.InputType)
		if err != nil {
			return nil, fmt.Errorf("koba/openapi: %s request schema: %w", description.MethodName, err)
		}
		operation.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().
			WithRequired(true).
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
	if description.InputType != nil {
		operation.AddResponse(http.StatusBadRequest, errorResponse("Invalid request"))
	}
	operation.AddResponse(http.StatusInternalServerError, errorResponse("Internal server error"))
	statusCodes := make([]int, 0, len(documentation.Responses))
	for status := range documentation.Responses {
		statusCodes = append(statusCodes, status)
	}
	sort.Ints(statusCodes)
	for _, status := range statusCodes {
		response := documentation.Responses[status]
		if status != successStatus && (status < 400 || status > 599) {
			return nil, fmt.Errorf(
				"koba/openapi: %s documents HTTP %d, but its resolved success status is %d",
				description.MethodName,
				status,
				successStatus,
			)
		}
		responseDescription := strings.TrimSpace(response.Description)
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
		} else {
			operation.AddResponse(status, errorResponse(responseDescription))
		}
	}

	if documentation.Public {
		empty := openapi3.SecurityRequirements{}
		operation.Security = &empty
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

func renderParameter(description fb.ParameterDocumentation, schemas openapi3.Schemas, customizer SchemaCustomizer) (*openapi3.Parameter, error) {
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
	generator := newSchemaGenerator(schemas, customizer)
	schema, err := schemaForType(generator, schemas, description.GoType)
	if err != nil {
		return nil, err
	}
	schema = constrainedSchema(schema, description)
	parameter.Description = strings.TrimSpace(description.Description)
	parameter.Required = description.Required || description.In == fb.ParameterInPath
	parameter.Deprecated = description.Deprecated
	parameter.Schema = schema
	parameter.Example = description.Example
	return parameter, nil
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

func newSchemaGenerator(schemas openapi3.Schemas, customizer SchemaCustomizer) *openapi3gen.Generator {
	return openapi3gen.NewGenerator(
		openapi3gen.UseAllExportedFields(),
		openapi3gen.CreateComponentSchemas(openapi3gen.ExportComponentSchemasOptions{
			ExportComponentSchemas: true,
			ExportTopLevelSchema:   true,
			ExportGenerics:         true,
		}),
		openapi3gen.CreateTypeNameGenerator(componentName),
		openapi3gen.SchemaCustomizer(func(name string, valueType reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
			if valueType == reflect.TypeFor[fbtypes.Time]() {
				*schema = *openapi3.NewDateTimeSchema()
			}
			if customizer != nil {
				if err := customizer(SchemaContext{Name: name, GoType: valueType, StructTag: tag, Schema: schema}); err != nil {
					return err
				}
			}
			// openapi3gen only exports component values with object properties.
			// Register all named struct schemas here as well so empty structs and
			// custom scalar marshalers retain resolvable component references.
			if schemas != nil && valueType.Kind() == reflect.Struct && valueType != reflect.TypeFor[time.Time]() && valueType.Name() != "" {
				if schema.Type == nil {
					schema.Type = &openapi3.Types{"object"}
				}
				schemas[componentName(valueType)] = &openapi3.SchemaRef{Value: schema}
			}
			return nil
		}),
	)
}

func schemaForType(generator *openapi3gen.Generator, schemas openapi3.Schemas, valueType reflect.Type) (*openapi3.SchemaRef, error) {
	if valueType == nil {
		return nil, fmt.Errorf("Go type is nil")
	}
	underlying := valueType
	for underlying.Kind() == reflect.Pointer {
		underlying = underlying.Elem()
	}
	if underlying.Kind() == reflect.Interface {
		return &openapi3.SchemaRef{Value: openapi3.NewSchema()}, nil
	}
	value := reflect.Zero(valueType).Interface()
	// openapi3gen expects a pointer when exporting a top-level struct into the
	// supplied components map. Passing the struct value can return a component
	// $ref without registering the referenced schema.
	if valueType.Kind() == reflect.Struct {
		value = reflect.New(valueType).Interface()
	}
	ref, err := generator.NewSchemaRefForValue(value, schemas)
	if err != nil {
		return nil, err
	}
	applyBindingConstraints(valueType, ref, schemas, make(map[reflect.Type]struct{}))
	return ref, nil
}

func responseSchemaForType(generator *openapi3gen.Generator, schemas openapi3.Schemas, valueType reflect.Type) (*openapi3.SchemaRef, error) {
	ref, err := schemaForType(generator, schemas, valueType)
	if err != nil {
		return nil, err
	}
	if valueType.Kind() != reflect.Pointer {
		return ref, nil
	}
	return &openapi3.SchemaRef{Value: &openapi3.Schema{
		Nullable: true,
		AllOf:    openapi3.SchemaRefs{ref},
	}}, nil
}

func applyBindingConstraints(valueType reflect.Type, ref *openapi3.SchemaRef, schemas openapi3.Schemas, seen map[reflect.Type]struct{}) {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if _, visited := seen[valueType]; visited {
		return
	}
	schema := resolvedSchema(ref, schemas)
	if schema == nil {
		return
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if valueType == reflect.TypeFor[time.Time]() || valueType == reflect.TypeFor[fbtypes.Time]() {
			return
		}
		seen[valueType] = struct{}{}
		for _, field := range reflect.VisibleFields(valueType) {
			if field.PkgPath != "" {
				continue
			}
			jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonName == "-" {
				continue
			}
			if field.Anonymous && jsonName == "" {
				// Promoted fields are also returned by VisibleFields and are
				// processed against this parent schema individually.
				continue
			}
			if jsonName == "" {
				jsonName = field.Name
			}
			property := schema.Properties[jsonName]
			if property == nil {
				continue
			}
			if bindingRequiresValue(field.Tag.Get("binding")) {
				schema.Required = appendUnique(schema.Required, jsonName)
			}
			applyBindingConstraints(field.Type, property, schemas, seen)
		}
		sort.Strings(schema.Required)
	case reflect.Array, reflect.Slice:
		if schema.Items != nil {
			applyBindingConstraints(valueType.Elem(), schema.Items, schemas, seen)
		}
	case reflect.Map:
		if schema.AdditionalProperties.Schema != nil {
			applyBindingConstraints(valueType.Elem(), schema.AdditionalProperties.Schema, schemas, seen)
		}
	}
}

func resolvedSchema(ref *openapi3.SchemaRef, schemas openapi3.Schemas) *openapi3.Schema {
	if ref == nil {
		return nil
	}
	if ref.Value != nil {
		return ref.Value
	}
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref.Ref, prefix) {
		if component := schemas[strings.TrimPrefix(ref.Ref, prefix)]; component != nil {
			return component.Value
		}
	}
	return nil
}

func bindingRequiresValue(binding string) bool {
	for _, rule := range strings.Split(binding, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "dive" {
			return false
		}
		if rule == "required" {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

var componentCharacter = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func componentName(valueType reflect.Type) string {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	name := valueType.Name()
	if name == "" {
		name = valueType.String()
	}
	if packagePath := valueType.PkgPath(); packagePath != "" {
		// Use the complete package path. Package basenames are not unique, and
		// silently reusing one component for two distinct Go types corrupts the
		// generated contract.
		name = packagePath + "." + name
	}
	return strings.Trim(componentCharacter.ReplaceAllString(name, "_"), "_")
}

func renderSecurityScheme(description SecurityScheme) (*openapi3.SecurityScheme, error) {
	scheme := &openapi3.SecurityScheme{
		Type:         strings.TrimSpace(description.Type),
		Description:  strings.TrimSpace(description.Description),
		In:           strings.TrimSpace(description.In),
		Name:         strings.TrimSpace(description.Parameter),
		Scheme:       strings.TrimSpace(description.Scheme),
		BearerFormat: strings.TrimSpace(description.BearerFormat),
	}
	switch scheme.Type {
	case SecurityTypeAPIKey:
		if scheme.Name == "" || scheme.In == "" {
			return nil, fmt.Errorf("koba/openapi: apiKey security scheme %q requires Parameter and In", description.Name)
		}
	case SecurityTypeHTTP:
		if scheme.Scheme == "" {
			return nil, fmt.Errorf("koba/openapi: HTTP security scheme %q requires Scheme", description.Name)
		}
	default:
		return nil, fmt.Errorf("koba/openapi: security scheme %q has unsupported type %q", description.Name, scheme.Type)
	}
	if strings.TrimSpace(description.ValuePrefix) != "" {
		// Prefix whitespace is semantically significant for values such as
		// "Token <credential>" and must survive generation unchanged.
		scheme.Extensions = map[string]any{"x-auth-value-prefix": description.ValuePrefix}
	}
	return scheme, nil
}

func renderSecurityRequirements(requirements []SecurityRequirement, known map[string]struct{}) (openapi3.SecurityRequirements, error) {
	if requirements == nil {
		return nil, nil
	}
	result := make(openapi3.SecurityRequirements, 0, len(requirements))
	for index, alternative := range requirements {
		if len(alternative) == 0 {
			return nil, fmt.Errorf("koba/openapi: security requirement %d is empty; use Public for an unauthenticated operation", index+1)
		}
		rendered := make(openapi3.SecurityRequirement, len(alternative))
		for _, value := range alternative {
			name := strings.TrimSpace(value)
			if _, exists := known[name]; !exists {
				return nil, fmt.Errorf("koba/openapi: unknown security scheme %q", name)
			}
			if _, duplicate := rendered[name]; duplicate {
				return nil, fmt.Errorf("koba/openapi: duplicate security scheme %q in requirement %d", name, index+1)
			}
			rendered[name] = []string{}
		}
		result = append(result, rendered)
	}
	return result, nil
}

func errorResponse(description string) *openapi3.Response {
	return openapi3.NewResponse().
		WithDescription(description).
		WithJSONSchemaRef(&openapi3.SchemaRef{Ref: "#/components/schemas/ErrorResponse"})
}

func errorResponseSchema() *openapi3.Schema {
	status := openapi3.NewIntegerSchema()
	status.Description = "HTTP status code returned by the transport."
	errorName := openapi3.NewStringSchema()
	errorName.Description = "Lowercase HTTP status name with spaces replaced by underscores."
	message := openapi3.NewStringSchema()
	message.Description = "Human-readable explanation of the failure."

	schema := openapi3.NewObjectSchema().
		WithProperty("status", status).
		WithProperty("error", errorName).
		WithProperty("message", message).
		WithRequired([]string{"status", "error", "message"})
	schema.Description = "Standard error envelope emitted by the koba HTTP adapter."
	return schema
}

var ginParameter = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_]*)`)

func normalizePath(route string) string {
	route = strings.TrimSpace(route)
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	return ginParameter.ReplaceAllString(route, `${1}{${2}}`)
}

var pathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

func validatePathParameters(methodName, route string, parameters openapi3.Parameters) error {
	declared := make(map[string]struct{})
	for _, parameter := range parameters {
		if parameter.Value != nil && parameter.Value.In == openapi3.ParameterInPath {
			declared[parameter.Value.Name] = struct{}{}
		}
	}
	for _, match := range pathParameter.FindAllStringSubmatch(route, -1) {
		if _, exists := declared[match[1]]; !exists {
			return fmt.Errorf("koba/openapi: %s route %s is missing path parameter metadata for %q", methodName, route, match[1])
		}
		delete(declared, match[1])
	}
	for name := range declared {
		return fmt.Errorf("koba/openapi: %s documents path parameter %q which is absent from %s", methodName, name, route)
	}
	return nil
}

func defaultTag(route string) string {
	segments := strings.Split(strings.Trim(route, "/"), "/")
	for _, segment := range segments {
		if segment == "" || strings.HasPrefix(segment, "{") || regexp.MustCompile(`^v[0-9]+$`).MatchString(segment) {
			continue
		}
		return segment
	}
	return "api"
}

var identifierBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func splitIdentifier(value string) string {
	value = identifierBoundary.ReplaceAllString(value, `$1 $2`)
	value = strings.ReplaceAll(value, "_", " ")
	return strings.TrimSpace(value)
}
