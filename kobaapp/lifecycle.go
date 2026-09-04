package kobaapp

import (
	"bytes"
	"fmt"
	"os"
)

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
