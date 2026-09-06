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
	ConsumedEvent    domain.KafkaEvent
	CreateError      error
	DeleteError      error
	ConsumeError     error
}

type createRequestContract struct {
	Subject fb.Binding[string]
	Payload fb.BodyObjectBinding[createPayloadContract]
}

type createPayloadContract struct {
	Name fb.Binding[string]
}

type composeDetailsContract struct {
	Note fb.Binding[string]
}

type composePayloadContract struct {
	Name      fb.Binding[string]
	Details   composeDetailsContract
	Latitude  fb.Binding[*float64]
	Longitude fb.Binding[*float64]
}

type composeRequestContract struct {
	ID       fb.Binding[string]
	PageSize fb.Binding[*domain.PageSize]
	Payload  fb.BodyObjectBinding[composePayloadContract]
}

type responseContract struct {
	ID fb.ResponseValueBinding[string]
}

type socketInputContract struct {
	Message fb.MessageValueBinding[string]
}

type socketOutputContract struct {
	ID      fb.MessageValueBinding[string]
	Message fb.MessageValueBinding[string]
}

type tagDetailsContract struct {
	Code fb.Binding[string]
}

type tagContract struct {
	Name    fb.Binding[string]
	Kind    fb.Binding[string]
	Details tagDetailsContract
}

type configurationContract struct {
	Attributes fb.Binding[map[string]any]
	Tags       fb.Binding[[]domain.Tag]
}

func (api *API) Create(_ context.Context, request domain.CreateRequest) (*domain.Response, error) {
	api.CreateCount++
	api.PrincipalID = request.Subject
	return &domain.Response{ID: request.Payload.Name}, api.CreateError
}

func (api *API) Compose(_ context.Context, request domain.ComposeRequest) domain.Response {
	api.ComposeCount++
	api.Composed = request
	return domain.Response{ID: request.ID}
}

func (*API) Nil() *domain.Response { return nil }

func (*API) Replace(ids domain.IDs) domain.IDs { return ids }

func (*API) Configure(configuration domain.Configuration) domain.Configuration {
	return configuration
}

func (*API) Update(request domain.Request) domain.Response {
	return domain.Response{ID: request.Name}
}

func (api *API) Delete(ctx context.Context) error {
	api.DeletedID = http_common.PathParametersFromContext(ctx)["id"]
	return api.DeleteError
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

func (*API) Connect(ctx context.Context) error {
	return fb.AcceptWebSocket[domain.WebSocketInput, domain.WebSocketOutput](ctx, func(
		_ context.Context,
		socket fb.SocketConnection[domain.WebSocketInput, domain.WebSocketOutput],
	) error {
		message, err := socket.Receive()
		if err != nil {
			return err
		}
		return socket.Send(domain.WebSocketOutput{ID: "fixture", Message: message.Message})
	})
}

func (api *API) Consume(_ context.Context, event domain.KafkaEvent) error {
	api.ConsumedEvent = event
	return api.ConsumeError
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
		"Create":    "/fixtures",
		"Nil":       "/fixtures/nil",
		"Replace":   "/fixtures/ids",
		"Update":    "/fixtures/:id",
		"Delete":    "/fixtures/:id",
		"Inspect":   "/fixtures/:id/inspect",
		"Connect":   "/fixtures/connect",
		"Configure": "/fixtures/configuration",
	}
}

func (*API) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name:        "static-fixture",
		Description: "Reflection and generated runtime parity fixture.",
		Document: &fb.APIDocument{
			Title:   "Static Fixture API",
			Version: "1.0.0",
			SecuritySchemes: []fb.APISecurityScheme{
				{
					Name:         "bearerAuth",
					Type:         fb.SecurityTypeHTTP,
					Scheme:       "bearer",
					BearerFormat: "JWT",
				},
			},
		},
		Operations: map[string]fb.OperationDocumentation{
			"Consume": {
				OperationID: "consumeStaticFixtureEvent",
				Summary:     "Consume a fixture event",
				Description: "Processes one JSON fixture event delivered by Kafka.",
				Tags:        []string{"events"},
			},
			"Connect": {
				OperationID: "connectStaticFixture",
				Summary:     "Connect fixture transport",
			},
			"Compose": {
				Route:       "/fixtures/:id/compose",
				Method:      http.MethodPatch,
				OperationID: "composeStaticFixture",
			},
			"Configure": {
				Method:      http.MethodPut,
				OperationID: "configureStaticFixture",
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
		"Consume": {
			Transport: fb.KafkaConsumer("static-fixture.events"),
		},
		"Connect": {
			Transport: fb.WebSocket(fb.Socket[
				domain.WebSocketInput,
				domain.WebSocketOutput,
			]().WithMessages(
				fb.ClientMessage("Message accepted from the fixture client.", socketInputContract{
					Message: fb.MessageValue[string](fb.Required(), fb.MinLength(1)).Document(
						"Client-authored fixture message.",
					),
				}),
				fb.ServerMessage("Message delivered by the fixture server.", socketOutputContract{
					ID: fb.MessageValue[string](fb.Required(), fb.WithFormat("fixture-id")).Document(
						"Stable fixture response identifier.",
					),
					Message: fb.MessageValue[string](fb.Required()).Document(
						"Echoed fixture message.",
					),
				}),
			)),
		},
		"Compose": {
			Request: composeRequestContract{
				ID: fb.Path[string]("id", fb.MinLength(1)).Document(
					"Fixture ID",
				),
				PageSize: fb.Query[*domain.PageSize](
					"page_size",
					fb.Min(domain.PageSize(1)),
					fb.Max(domain.PageSize(100)),
				).Document(
					"Optional page size",
				),
				Payload: fb.BodyObject(composePayloadContract{
					Name: fb.Value[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)).Document(
						"Composition name.", fb.Example("fixture"),
					),
					Details: composeDetailsContract{
						Note: fb.Value[string](fb.Required(), fb.MaxLength(128)).Document("Composition note."),
					},
					Latitude:  fb.Value[*float64](fb.Nullable(), fb.Min(-90.0), fb.Max(90.0)),
					Longitude: fb.Value[*float64](fb.Nullable(), fb.Min(-180.0), fb.Max(180.0)),
				}, fb.Required()).Document(
					"Composition payload.",
					fb.Example(domain.ComposePayload{
						Name:    "fixture",
						Details: domain.ComposeDetails{Note: "nested fixture"},
					}),
				).WithRules(
					fb.AllOrNone("Latitude", "Longitude"),
					fb.SameNullState("Latitude", "Longitude"),
				),
			},
			Response: responseContract{
				ID: fb.ResponseValue[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)).Document(
					"Composed fixture identifier.", fb.Example("fixture-1"),
				),
			},
		},
		"Create": {
			Request: createRequestContract{
				Subject: fb.Subject(),
				Payload: fb.BodyObject(createPayloadContract{
					Name: fb.Value[string](fb.Required(), fb.MinLength(1), fb.MaxLength(64)),
				}, fb.Required()),
			},
			Authorization: fb.AuthorizationContract{
				RequiredScopes: []string{"create:fixtures"},
			},
		},
		"Configure": {
			Request: fb.BodyObject(configurationContract{
				Attributes: fb.Value[map[string]any](fb.Default(map[string]any{
					"presentation": map[string]any{"labels": []any{}},
				})).Document("Free-form configuration attributes."),
				Tags: fb.Value[[]domain.Tag](
					fb.Default([]domain.Tag{}),
					fb.UniqueItems(),
					fb.Elements(fb.ElementObject[domain.Tag](tagContract{
						Name: fb.Value[string](fb.Required(), fb.MinLength(1)).Document("Tag name."),
						Kind: fb.Value[string](fb.Required(), fb.OneOf("system", "user")).Document("Tag kind."),
						Details: tagDetailsContract{
							Code: fb.Value[string](fb.Required(), fb.WithFormat("slug")).Document("Nested tag code."),
						},
					}).Document("Configured tag.")),
				).Document("Configured tags."),
			}, fb.Required()),
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
		"Replace": {
			Request: fb.Body[domain.IDs](
				fb.Required(),
				fb.MinItems(1),
				fb.MaxItems(3),
				fb.UniqueItems(),
				fb.Elements(fb.Element[string](fb.MinLength(1)).Document("Fixture identifier.")),
			),
		},
	}}
}
