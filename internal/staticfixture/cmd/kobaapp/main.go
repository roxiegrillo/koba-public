package main

import (
	"fmt"
	"os"

	"github.com/roxiegrillo/koba/v2/kobaapp"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/openapi"
)

func main() {
	command := kobaapp.NewCommand(kobaapp.Definition{
		ID:             "static-fixture",
		APIs:           []any{(*staticfixture.API)(nil)},
		OpenAPI:        openapi.Config{Title: "Static Fixture API", Version: "1.0.0"},
		OpenAPIOutput:  "internal/staticfixture/openapi.json",
		ManifestOutput: "internal/staticfixture/koba.lock.json",
		StaticTargets: []kobaapp.StaticTarget{{
			PackageName: "staticfixture",
			PackagePath: "github.com/roxiegrillo/koba/v2/internal/staticfixture",
			Output:      "internal/staticfixture/zz_koba.gen.go",
			APIs:        []any{(*staticfixture.API)(nil)},
		}},
	})
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
