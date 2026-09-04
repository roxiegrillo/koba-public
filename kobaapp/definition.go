// Package kobaapp defines the build-time contract for a service which
// uses koba. It coordinates deterministic static bindings,
// OpenAPI, and an ownership manifest without changing the runtime API.
package kobaapp

import "github.com/roxiegrillo/koba/v2/openapi"

const (
	ManifestSchemaVersion = 1
	GeneratorVersion      = "1"
)

// StaticTarget describes one generated Go file. APIs in different Go
// packages must use separate targets because generated provider methods live
// beside their receiver types.
type StaticTarget struct {
	PackageName string
	PackagePath string
	Output      string
	BuildTag    string
	APIs        []any
}

// Definition is the single build-time description of one Koba app.
// API values may be typed nil pointers when Routes and APIDocumentation are
// metadata-only, as recommended by the framework convention.
type Definition struct {
	ID             string
	APIs           []any
	OpenAPI        openapi.Config
	OpenAPIOutput  string
	ManifestOutput string
	StaticTargets  []StaticTarget
}

// Manifest records exactly which committed artifacts were derived from a
// service definition. It intentionally does not include its own digest.
type Manifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	ServiceID     string             `json:"serviceId"`
	Generator     ManifestGenerator  `json:"generator"`
	Artifacts     []ManifestArtifact `json:"artifacts"`
}

type ManifestGenerator struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}

type ManifestArtifact struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	BuildTag string `json:"buildTag,omitempty"`
}

type generatedArtifact struct {
	kind     string
	path     string
	buildTag string
	contents []byte
}
