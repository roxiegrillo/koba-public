package kobaapp

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand returns the small Cobra program a service places under
// cmd/kobaapp. With no flags it regenerates artifacts; --check is the
// read-only CI mode and --validate checks metadata without consulting files.
func NewCommand(definition Definition) *cobra.Command {
	check := false
	validate := false
	command := &cobra.Command{
		Use:           "kobaapp",
		Short:         "Generate and verify koba service artifacts",
		SilenceErrors: true,
		SilenceUsage:  true,
		Example: `  kobaapp
  kobaapp --check
  kobaapp --validate`,
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
