package koba_test

import (
	"net/http"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/kobaapp"
	"github.com/roxiegrillo/koba/v2/middleware"
)

func staticFixtureDefinition() kobaapp.Definition {
	return kobaapp.Definition{
		ID:             "static-fixture",
		APIs:           []any{(*staticfixture.API)(nil)},
		OpenAPIOutput:  "internal/staticfixture/openapi.json",
		ManifestOutput: "internal/staticfixture/koba.lock.json",
		StaticTargets: []kobaapp.StaticTarget{{
			PackageName: "staticfixture",
			PackagePath: "github.com/roxiegrillo/koba/v2/internal/staticfixture",
			Output:      "internal/staticfixture/zz_koba.gen.go",
			APIs:        []any{(*staticfixture.API)(nil)},
		}},
	}
}

func staticFixtureAuthentication() http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		ctx := middleware.WithPrincipal(request.Context(), middleware.Principal{
			UserID: "fixture-user",
			Scope:  "create:fixtures",
		})
		*request = *request.WithContext(ctx)
	})
}

func newStaticFixtureApp() fb.KobaApp {
	return fb.MakeRawAppWithOptions(fb.WithAuthenticationMiddleware(staticFixtureAuthentication()))
}
