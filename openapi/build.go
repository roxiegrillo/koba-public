package openapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

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
