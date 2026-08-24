package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// CommandOptions configure the reusable Cobra command embedded by a consumer
// repository's small api-contract program.
type CommandOptions struct {
	Config        Config
	APIs          []any
	DefaultOutput string
}

// NewCommand returns a Cobra command which generates or verifies a committed
// OpenAPI JSON artifact. The API values are only reflected; endpoints are not
// invoked and application infrastructure does not need to be initialized.
func NewCommand(options CommandOptions) *cobra.Command {
	output := strings.TrimSpace(options.DefaultOutput)
	if output == "" {
		output = "openapi.json"
	}
	check := false
	compact := false
	command := &cobra.Command{
		Use:           "api-contract",
		Short:         "Generate the service OpenAPI contract",
		SilenceErrors: true,
		SilenceUsage:  true,
		Example: `  api-contract --output docs/openapi.json
  api-contract --check --output docs/openapi.json
  api-contract --output -`,
		RunE: func(command *cobra.Command, _ []string) error {
			contents, err := GenerateJSON(options.Config, compact, options.APIs...)
			if err != nil {
				return err
			}
			if output == "-" {
				if check {
					return fmt.Errorf("--check requires a file output, not stdout")
				}
				_, err = command.OutOrStdout().Write(contents)
				return err
			}
			if check {
				return checkGeneratedFile(output, contents)
			}
			return writeGeneratedFile(output, contents)
		},
	}
	command.Flags().StringVarP(&output, "output", "o", output, "OpenAPI JSON output path, or - for stdout")
	command.Flags().BoolVar(&check, "check", false, "fail if the output file does not exactly match generated content")
	command.Flags().BoolVar(&compact, "compact", false, "write compact JSON instead of indented JSON")
	return command
}

// GenerateJSON renders a validated OpenAPI document as deterministic JSON with
// one trailing newline.
func GenerateJSON(config Config, compact bool, apis ...any) ([]byte, error) {
	document, err := Generate(config, apis...)
	if err != nil {
		return nil, err
	}
	var contents []byte
	if compact {
		contents, err = json.Marshal(document)
	} else {
		contents, err = json.MarshalIndent(document, "", "  ")
	}
	if err != nil {
		return nil, fmt.Errorf("koba/openapi: encode document: %w", err)
	}
	return append(contents, '\n'), nil
}

func checkGeneratedFile(filename string, expected []byte) error {
	actual, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("OpenAPI contract %q is missing; regenerate it", filename)
		}
		return fmt.Errorf("read OpenAPI contract %q: %w", filename, err)
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("OpenAPI contract %q is stale; regenerate it", filename)
	}
	return nil
}

func writeGeneratedFile(filename string, contents []byte) error {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create OpenAPI output directory %q: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".openapi-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary OpenAPI contract: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set OpenAPI contract permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary OpenAPI contract: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary OpenAPI contract: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary OpenAPI contract: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace OpenAPI contract %q: %w", filename, err)
	}
	return nil
}
