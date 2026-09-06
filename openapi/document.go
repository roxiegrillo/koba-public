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

// Server aliases the neutral documentation type retained for Config backward
// compatibility. New functional APIs normally declare servers in
// APIDocumentation.Document.
type Server = fb.APIServer

// SecurityScheme aliases the neutral documentation type retained for Config
// backward compatibility. New APIs normally declare schemes in
// APIDocumentation.Document.
type SecurityScheme = fb.APISecurityScheme

const (
	SecurityTypeAPIKey = fb.SecurityTypeAPIKey
	SecurityTypeHTTP   = fb.SecurityTypeHTTP
)

// SecurityRequirement aliases the transport-neutral contract type for
// convenient use in OpenAPI configuration.
type SecurityRequirement = fb.SecurityRequirement

// SchemaContext is passed to a project's optional schema customizer. It
// provides the Go type and struct tag which produced an OpenAPI schema while
// keeping customization at the adapter boundary. Descriptor-derived protobuf
// socket schemas provide their generated pointer Go type and an empty tag.
type SchemaContext struct {
	Name      string
	GoType    reflect.Type
	StructTag reflect.StructTag
	Schema    *openapi3.Schema
}

// SchemaCustomizer is the legacy escape hatch for project-specific JSON
// encodings which reflection and API contracts cannot infer, such as custom
// marshalers. New APIs should declare field constraints and prose on request
// and response bindings instead of using a customizer for ordinary schemas.
type SchemaCustomizer func(SchemaContext) error

// Config contains adapter and legacy document configuration. New functional
// APIs should place title, version, servers, schemes, and default security in
// APIDocumentation.Document. SchemaCustomizer and DisableDefaultProbes remain
// adapter configuration. A document cannot declare metadata in both places.
type Config struct {
	Title            string
	Version          string
	Description      string
	Servers          []Server
	SecuritySchemes  []SecurityScheme
	DefaultSecurity  []SecurityRequirement
	ExternalDocsURL  string
	SchemaCustomizer SchemaCustomizer
	// DisableDefaultProbes omits the framework-owned /healthz and /readyz
	// operations from build-time OpenAPI generation. Use it together with the
	// runtime WithoutDefaultProbes option.
	DisableDefaultProbes bool
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
	if !config.DisableDefaultProbes {
		descriptions = withDefaultProbeDescriptions(descriptions)
	}
	return Build(config, descriptions)
}

// GenerateFromApp builds a document from the neutral descriptions captured by
// KobaApp.AddApi. Framework probe descriptions are included only when
// the runtime exposes them through WithOperationalProbes.
func GenerateFromApp(config Config, app fb.WithAPIDescriptions) (*openapi3.T, error) {
	if app == nil {
		return nil, fmt.Errorf("koba/openapi: app is nil")
	}
	descriptions := app.RegisteredAPIDescriptions()
	operational, exposesProbeState := app.(fb.WithOperationalProbes)
	includeDefaultProbes := !config.DisableDefaultProbes && exposesProbeState && operational.DefaultProbesEnabled()
	if includeDefaultProbes {
		descriptions = withDefaultProbeDescriptions(descriptions)
	}
	return Build(config, descriptions)
}
