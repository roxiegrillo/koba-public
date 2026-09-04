// Package staticfixture is a real generated consumer used to prove that the
// reflected and koba_static execution paths stay equivalent.
package staticfixture

import (
	"context"
	"net/http"

	fb "github.com/roxiegrillo/koba/v2"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
	"github.com/roxiegrillo/koba/v2/middleware"
)

type API struct {
	DeletedID        string
	PrincipalID      string
	CreateCount      int
	InspectCount     int
	InspectedID      string
	PageSize         domain.PageSize
	PageSizeProvided bool
	TraceID          string
	SessionID        string
	ComposeCount     int
	Composed         domain.ComposeRequest
}

type createRequestContract struct {
	Name fb.Binding[string]
}

type composeDetailsContract struct {
	Note fb.Binding[string]
}

type composePayloadContract struct {
	Name    fb.Binding[string]
	Details composeDetailsContract
}

type composeRequestContract struct {
	ID       fb.Binding[string]
	PageSize fb.Binding[*domain.PageSize]
	Payload  fb.BodyObjectBinding[composePayloadContract]
}

func (api *API) Create(ctx context.Context, request domain.Request) *domain.Response {
	api.CreateCount++
	if principal, ok := middleware.PrincipalFromContext(ctx); ok {
		api.PrincipalID = principal.UserID
	}
	return &domain.Response{ID: request.Name}
}

func (api *API) Compose(_ context.Context, request domain.ComposeRequest) domain.Response {
	api.ComposeCount++
	api.Composed = request
	return domain.Response{ID: request.ID}
}

func (*API) Nil() *domain.Response { return nil }

func (*API) Replace(ids domain.IDs) domain.IDs { return ids }

func (*API) Update(request domain.Request) domain.Response {
	return domain.Response{ID: request.Name}
}

func (api *API) Delete(ctx context.Context) {
	api.DeletedID = http_common.PathParametersFromContext(ctx)["id"]
}

func (api *API) Inspect(ctx context.Context) domain.Response {
	api.InspectCount++
	api.InspectedID = mustInput[string](ctx, "fixtureID")
	api.PageSize = mustInput[domain.PageSize](ctx, "pageSize")
	api.PageSizeProvided = fb.InputWasProvided(ctx, "pageSize")
	api.TraceID = mustInput[string](ctx, "traceID")
	api.SessionID, _ = fb.InputValue[string](ctx, "sessionID")
	return domain.Response{ID: api.InspectedID}
}

func mustInput[T any](ctx context.Context, identifier string) T {
	value, exists := fb.InputValue[T](ctx, identifier)
	if !exists {
		panic("missing fixture input " + identifier)
	}
	return value
}

func (*API) Routes() map[string]string {
	return map[string]string{
		"Create":  "/fixtures",
		"Compose": "/fixtures/:id/compose",
		"Nil":     "/fixtures/nil",
		"Replace": "/fixtures/ids",
		"Update":  "/fixtures/:id",
		"Delete":  "/fixtures/:id",
		"Inspect": "/fixtures/:id/inspect",
	}
}

func (*API) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name:        "static-fixture",
		Description: "Reflection and generated runtime parity fixture.",
		Operations: map[string]fb.OperationDocumentation{
			"Compose": {
				Method:      http.MethodPatch,
				OperationID: "composeStaticFixture",
				Parameters: []fb.ParameterDocumentation{
					{Name: "id", In: fb.ParameterInPath, Description: "Fixture ID"},
					{Name: "page_size", In: fb.ParameterInQuery, Description: "Optional page size"},
				},
				RequestBodyDescription: "Deeply nested composition payload.",
			},
			"Create": {
				SuccessStatus: http.StatusCreated,
				OperationID:   "createStaticFixture",
				Security:      []fb.SecurityRequirement{{"bearerAuth"}},
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
			"Inspect": {
				OperationID: "inspectStaticFixture",
				Parameters: []fb.ParameterDocumentation{
					{Name: "id", In: fb.ParameterInPath, ContractInput: "fixtureID", Description: "Fixture ID"},
					{Name: "page_size", In: fb.ParameterInQuery, ContractInput: "pageSize", Description: "Page size"},
					{Name: "X-Trace-ID", In: fb.ParameterInHeader, ContractInput: "traceID", Description: "Trace ID"},
					{Name: "session", In: fb.ParameterInCookie, ContractInput: "sessionID", Description: "Session ID"},
				},
				Responses: map[int]fb.ResponseDocumentation{
					http.StatusOK: {
						Description: "Inspected fixture",
						Headers: map[string]fb.HeaderDocumentation{
							"X-Next-Page-Token": {Description: "Opaque continuation token"},
						},
					},
				},
				Security: []fb.SecurityRequirement{{"bearerAuth"}, {}},
			},
		},
	}
}

func (*API) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Compose": {
			Request: composeRequestContract{
				ID:       fb.Path[string]("id", fb.MinLength(1)),
				PageSize: fb.Query[*domain.PageSize]("page_size", fb.Min(domain.PageSize(1)), fb.Max(domain.PageSize(100))),
				Payload: fb.BodyObject(composePayloadContract{
					Name: fb.Value[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)),
					Details: composeDetailsContract{
						Note: fb.Value[string](fb.Required(), fb.MaxLength(128)),
					},
				}, fb.Required()),
			},
		},
		"Create": {
			Request: fb.BodyObject(createRequestContract{
				Name: fb.Value[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)),
			}, fb.Required()),
			Authorization: fb.AuthorizationContract{
				RequiredScopes: []string{"create:fixtures"},
			},
		},
		"Inspect": {Inputs: map[string]fb.InputContract{
			"fixtureID": fb.InputOf[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)),
			"pageSize": fb.InputOf[domain.PageSize](
				fb.Default(domain.PageSize(20)),
				fb.Min(domain.PageSize(1)),
				fb.Max(domain.PageSize(100)),
			),
			"traceID":   fb.InputOf[string](fb.Required(), fb.MinLength(1), fb.MaxLength(16)),
			"sessionID": fb.InputOf[string](fb.MaxLength(16)),
		}},
	}}
}
