package kobaapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/roxiegrillo/koba/v2/openapi"
	"github.com/roxiegrillo/koba/v2/staticgen"
)

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
