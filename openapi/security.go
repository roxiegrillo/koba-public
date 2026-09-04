package openapi

import (
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

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
			// An empty OpenAPI Security Requirement Object is the anonymous
			// alternative. Combined with a named alternative, it models optional
			// authentication.
			result = append(result, openapi3.SecurityRequirement{})
			continue
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
