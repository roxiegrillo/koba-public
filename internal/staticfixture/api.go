// Package staticfixture is a real generated consumer used to prove that the
// reflected and koba_static execution paths stay equivalent.
package staticfixture

import (
	"context"
	"net/http"

	fb "github.com/roxiegrillo/koba/v2"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
)

type API struct {
	DeletedID   string
	CreateCount int
}

func (api *API) Create(_ context.Context, request domain.Request) *domain.Response {
	api.CreateCount++
	return &domain.Response{ID: request.Name}
}

func (*API) Nil() *domain.Response { return nil }

func (*API) Replace(ids domain.IDs) domain.IDs { return ids }

func (*API) Update(request domain.Request) domain.Response {
	return domain.Response{ID: request.Name}
}

func (api *API) Delete(ctx context.Context) {
	api.DeletedID = http_common.PathParametersFromContext(ctx)["id"]
}

func (*API) Routes() map[string]string {
	return map[string]string{
		"Create":  "/fixtures",
		"Nil":     "/fixtures/nil",
		"Replace": "/fixtures/ids",
		"Update":  "/fixtures/:id",
		"Delete":  "/fixtures/:id",
	}
}

func (*API) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name:        "static-fixture",
		Description: "Reflection and generated runtime parity fixture.",
		Operations: map[string]fb.OperationDocumentation{
			"Create": {
				SuccessStatus: http.StatusCreated,
				OperationID:   "createStaticFixture",
			},
			"Nil": {OperationID: "getNilStaticFixture"},
			"Replace": {
				Method:      http.MethodPut,
				OperationID: "replaceStaticFixtureIDs",
			},
			"Update": {
				Method:      http.MethodPatch,
				OperationID: "updateStaticFixture",
				Parameters: []fb.ParameterDocumentation{
					fb.ParameterOf[string]("id", fb.ParameterInPath, "Fixture ID", true),
				},
			},
			"Delete": {
				Method:      http.MethodDelete,
				OperationID: "deleteStaticFixture",
				Parameters: []fb.ParameterDocumentation{
					fb.ParameterOf[string]("id", fb.ParameterInPath, "Fixture ID", true),
				},
			},
		},
	}
}
