// Package openapi renders koba's neutral API descriptions as an
// OpenAPI document. Application APIs remain free of OpenAPI-specific types.
package openapi

import (
	"fmt"
	"reflect"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
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
