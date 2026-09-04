// Package staticgen generates reflection-free operation invokers for
// koba APIs. Reflection is used only while running the generator;
// the emitted provider calls API methods directly.
package staticgen

import (
	internalstaticgen "github.com/roxiegrillo/koba/v2/internal/staticgen"
)

const DefaultBuildTag = "koba_static"

// Config selects the API values emitted into one generated Go package. All API
// types must be named types from PackagePath. Typed nil pointers are supported.
type Config struct {
	PackageName string
	PackagePath string
	BuildTag    string
	APIs        []any
}

// Generate returns a gofmt-formatted generated provider source file.
func Generate(config Config) ([]byte, error) {
	return internalstaticgen.Generate(internalstaticgen.Config{
		PackageName: config.PackageName,
		PackagePath: config.PackagePath,
		BuildTag:    config.BuildTag,
		APIs:        config.APIs,
	})
}
