package kobaapp

import (
	"context"
	"path/filepath"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/openapi"
)

type definitionFixtureRequest struct {
	Name string `json:"name" binding:"required"`
}

type definitionFixtureResponse struct {
	ID string `json:"id"`
}

type definitionFixtureAPI struct{}

func (*definitionFixtureAPI) Create(_ context.Context, request definitionFixtureRequest) definitionFixtureResponse {
	return definitionFixtureResponse{ID: request.Name}
}

func (*definitionFixtureAPI) Routes() map[string]string {
	return map[string]string{"Create": "/items"}
}

func (*definitionFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name: "items",
		Operations: map[string]fb.OperationDocumentation{
			"Create": {
				OperationID: "itemsCreate",
				Summary:     "Create an item",
				Tags:        []string{"items"},
				Public:      true,
			},
		},
	}
}

func fixtureDefinition(directory string) Definition {
	api := (*definitionFixtureAPI)(nil)
	return Definition{
		ID:             "fixture",
		APIs:           []any{api},
		OpenAPI:        openapi.Config{Title: "Fixture API", Version: "1.0.0"},
		OpenAPIOutput:  filepath.Join(directory, "contract", "openapi.json"),
		ManifestOutput: filepath.Join(directory, "contract", "koba.lock.json"),
		StaticTargets: []StaticTarget{{
			PackageName: "kobaapp",
			PackagePath: "github.com/roxiegrillo/koba/v2/kobaapp",
			Output:      filepath.Join(directory, "zz_koba.gen.go"),
			APIs:        []any{api},
		}},
	}
}
