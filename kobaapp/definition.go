// Package kobaapp defines the build-time contract for a service which
// uses koba. It coordinates deterministic static bindings,
// OpenAPI, and an ownership manifest without changing the runtime API.
package kobaapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/roxiegrillo/koba/v2/openapi"
	"github.com/roxiegrillo/koba/v2/staticgen"
	"github.com/spf13/cobra"
)

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

// Validate checks all service metadata and generation inputs without writing
// files. OpenAPI generation is part of validation, so incomplete operation
// metadata and invalid schemas fail here rather than at runtime.
func Validate(definition Definition) error {
	_, err := buildArtifacts(definition)
	return err
}

// Generate writes every derived artifact atomically after all artifacts have
// first been generated and validated in memory.
func Generate(definition Definition) error {
	artifacts, err := buildArtifacts(definition)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := writeFileAtomically(artifact.path, artifact.contents); err != nil {
			return err
		}
	}
	return nil
}

// Check fails when a generated artifact is missing or differs byte-for-byte
// from the service definition. It never mutates the repository.
func Check(definition Definition) error {
	artifacts, err := buildArtifacts(definition)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		actual, err := os.ReadFile(artifact.path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("koba/kobaapp: generated artifact %q is missing", artifact.path)
			}
			return fmt.Errorf("koba/kobaapp: read generated artifact %q: %w", artifact.path, err)
		}
		if !bytes.Equal(actual, artifact.contents) {
			return fmt.Errorf("koba/kobaapp: generated artifact %q is stale", artifact.path)
		}
	}
	return nil
}

// NewCommand returns the small Cobra program a service places under
// cmd/koba-app. With no flags it regenerates artifacts; --check is the
// read-only CI mode and --validate checks metadata without consulting files.
func NewCommand(definition Definition) *cobra.Command {
	check := false
	validate := false
	command := &cobra.Command{
		Use:           "koba-app",
		Short:         "Generate and verify koba service artifacts",
		SilenceErrors: true,
		SilenceUsage:  true,
		Example: `  koba-app
  koba-app --check
  koba-app --validate`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if check && validate {
				return fmt.Errorf("--check and --validate cannot be used together")
			}
			if validate {
				return Validate(definition)
			}
			if check {
				return Check(definition)
			}
			return Generate(definition)
		},
	}
	command.Flags().BoolVar(&check, "check", false, "fail when committed generated artifacts are missing or stale")
	command.Flags().BoolVar(&validate, "validate", false, "validate service and API metadata without reading or writing artifacts")
	return command
}

func buildArtifacts(definition Definition) ([]generatedArtifact, error) {
	definition.ID = strings.TrimSpace(definition.ID)
	definition.OpenAPIOutput = filepath.Clean(strings.TrimSpace(definition.OpenAPIOutput))
	definition.ManifestOutput = filepath.Clean(strings.TrimSpace(definition.ManifestOutput))
	if definition.ID == "" {
		return nil, fmt.Errorf("koba/kobaapp: service ID is required")
	}
	if len(definition.APIs) == 0 {
		return nil, fmt.Errorf("koba/kobaapp: at least one API is required")
	}
	if len(definition.StaticTargets) == 0 {
		return nil, fmt.Errorf("koba/kobaapp: at least one static target is required")
	}
	if definition.OpenAPIOutput == "." || definition.OpenAPIOutput == "" {
		return nil, fmt.Errorf("koba/kobaapp: OpenAPI output is required")
	}
	if definition.ManifestOutput == "." || definition.ManifestOutput == "" {
		return nil, fmt.Errorf("koba/kobaapp: manifest output is required")
	}
	if samePath(definition.OpenAPIOutput, definition.ManifestOutput) {
		return nil, fmt.Errorf("koba/kobaapp: OpenAPI and manifest outputs must differ")
	}

	openAPIContents, err := openapi.GenerateJSON(definition.OpenAPI, false, definition.APIs...)
	if err != nil {
		return nil, err
	}
	artifacts := []generatedArtifact{{
		kind: "openapi", path: definition.OpenAPIOutput, contents: openAPIContents,
	}}
	seenOutputs := map[string]string{normalizedPath(definition.OpenAPIOutput): "openapi"}
	serviceAPIs := make(map[string]any, len(definition.APIs))
	for index, api := range definition.APIs {
		identity, err := apiIdentity(api)
		if err != nil {
			return nil, fmt.Errorf("koba/kobaapp: API %d: %w", index+1, err)
		}
		if _, duplicate := serviceAPIs[identity]; duplicate {
			return nil, fmt.Errorf("koba/kobaapp: duplicate service API type %s", identity)
		}
		serviceAPIs[identity] = api
	}
	coveredAPIs := make(map[string]struct{}, len(serviceAPIs))

	for index, target := range definition.StaticTargets {
		target.Output = filepath.Clean(strings.TrimSpace(target.Output))
		if target.Output == "." || target.Output == "" {
			return nil, fmt.Errorf("koba/kobaapp: static target %d output is required", index+1)
		}
		normalized := normalizedPath(target.Output)
		if previous, duplicate := seenOutputs[normalized]; duplicate {
			return nil, fmt.Errorf("koba/kobaapp: output %q is used by both %s and static bindings", target.Output, previous)
		}
		if samePath(target.Output, definition.ManifestOutput) {
			return nil, fmt.Errorf("koba/kobaapp: static target %q conflicts with the manifest output", target.Output)
		}
		canonicalAPIs := make([]any, 0, len(target.APIs))
		for apiIndex, api := range target.APIs {
			identity, err := apiIdentity(api)
			if err != nil {
				return nil, fmt.Errorf("koba/kobaapp: static target %q API %d: %w", target.Output, apiIndex+1, err)
			}
			canonicalAPI, exists := serviceAPIs[identity]
			if !exists {
				return nil, fmt.Errorf("koba/kobaapp: static target %q contains API %s which is not in Definition.APIs", target.Output, identity)
			}
			if _, duplicate := coveredAPIs[identity]; duplicate {
				return nil, fmt.Errorf("koba/kobaapp: API %s appears in more than one static target", identity)
			}
			coveredAPIs[identity] = struct{}{}
			canonicalAPIs = append(canonicalAPIs, canonicalAPI)
		}
		contents, err := staticgen.Generate(staticgen.Config{
			PackageName: target.PackageName,
			PackagePath: target.PackagePath,
			BuildTag:    target.BuildTag,
			APIs:        canonicalAPIs,
		})
		if err != nil {
			return nil, fmt.Errorf("koba/kobaapp: static target %q: %w", target.Output, err)
		}
		buildTag := strings.TrimSpace(target.BuildTag)
		if buildTag == "" {
			buildTag = staticgen.DefaultBuildTag
		}
		artifacts = append(artifacts, generatedArtifact{
			kind: "static-go", path: target.Output, buildTag: buildTag, contents: contents,
		})
		seenOutputs[normalized] = "static bindings"
	}
	for identity := range serviceAPIs {
		if _, covered := coveredAPIs[identity]; !covered {
			return nil, fmt.Errorf("koba/kobaapp: API %s has no static target", identity)
		}
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return normalizedPath(artifacts[i].path) < normalizedPath(artifacts[j].path)
	})
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		ServiceID:     definition.ID,
		Generator: ManifestGenerator{
			Module:  "github.com/roxiegrillo/koba/v2/kobaapp",
			Version: GeneratorVersion,
		},
		Artifacts: make([]ManifestArtifact, 0, len(artifacts)),
	}
	for _, artifact := range artifacts {
		manifest.Artifacts = append(manifest.Artifacts, ManifestArtifact{
			Kind:     artifact.kind,
			Path:     normalizedPath(artifact.path),
			SHA256:   digest(artifact.contents),
			BuildTag: artifact.buildTag,
		})
	}
	manifestContents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("koba/kobaapp: encode manifest: %w", err)
	}
	manifestContents = append(manifestContents, '\n')
	artifacts = append(artifacts, generatedArtifact{
		kind: "manifest", path: definition.ManifestOutput, contents: manifestContents,
	})
	return artifacts, nil
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizedPath(value string) string {
	return filepath.ToSlash(filepath.Clean(value))
}

func samePath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		return leftAbsolute == rightAbsolute
	}
	return normalizedPath(left) == normalizedPath(right)
}

func apiIdentity(api any) (string, error) {
	typeOfAPI := reflect.TypeOf(api)
	if typeOfAPI == nil {
		return "", fmt.Errorf("API value is nil; pass a value or typed nil pointer")
	}
	for typeOfAPI.Kind() == reflect.Pointer {
		typeOfAPI = typeOfAPI.Elem()
	}
	if typeOfAPI.Name() == "" || typeOfAPI.PkgPath() == "" {
		return "", fmt.Errorf("API type %s must be a named package type", typeOfAPI)
	}
	return typeOfAPI.PkgPath() + "." + typeOfAPI.Name(), nil
}

func writeFileAtomically(filename string, contents []byte) error {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("koba/kobaapp: create output directory %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".koba-app-*.tmp")
	if err != nil {
		return fmt.Errorf("koba/kobaapp: create temporary artifact: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: set artifact permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: write temporary artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("koba/kobaapp: sync temporary artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("koba/kobaapp: close temporary artifact: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("koba/kobaapp: replace artifact %q: %w", filename, err)
	}
	return nil
}
