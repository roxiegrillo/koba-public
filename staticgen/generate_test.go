package staticgen

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture"
	"github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/typepb"
)

var _ func(Config) ([]byte, error) = Generate

type generatorRequest struct {
	Name string `json:"name"`
}

type generatorResponse struct {
	ID string `json:"id"`
}

type generatorFixtureAPI struct{}

type generatorValueAPI struct{}

func (generatorValueAPI) Ping() generatorResponse { return generatorResponse{} }

type generatorVoidAPI struct{}

func (generatorVoidAPI) Delete() {}

type generatorWebSocketInput struct {
	Message string
}

type generatorWebSocketOutput struct {
	ID string
}

type generatorWebSocketAPI struct{}

type generatorDocumentedSocketContract struct {
	Name fb.MessageValueBinding[string]
}

type generatorDocumentedWebSocketAPI struct{}

func (*generatorDocumentedWebSocketAPI) Connect(context.Context) {}

func (*generatorDocumentedWebSocketAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Connect": {Transport: fb.WebSocket(fb.Socket[*typepb.Type, *typepb.Enum]().WithMessages(
			fb.ClientMessage("Client message.", generatorDocumentedSocketContract{
				Name: fb.MessageValue[string](fb.Required(), fb.MinLength(1)).Document(
					"Client name.", fb.Example("example.Client"),
				),
			}),
			fb.ServerMessage("Server message.", generatorDocumentedSocketContract{
				Name: fb.MessageValue[string](fb.Required()).Document("Server name."),
			}),
		))},
	}}
}

func (*generatorWebSocketAPI) Connect(context.Context) {}

func (*generatorWebSocketAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Connect": {Transport: fb.WebSocket(fb.Socket[
			*typepb.Type,
			*anypb.Any,
		]())},
	}}
}

type generatorCollectionAPI struct{}

func (*generatorCollectionAPI) Replace(context.Context, []string) {}

func (*generatorCollectionAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Replace": {
			Request: fb.Body[[]string](
				fb.Required(),
				fb.UniqueItems(),
				fb.Elements(fb.Element[string](fb.WithFormat("uuid")).Document(
					"Stable collection identifier.",
					fb.Example("49a8511f-561a-43f3-b04b-309411895786"),
				)),
			),
		},
	}}
}

type generatorStructuredCollectionItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type generatorStructuredCollectionItemContract struct {
	Name fb.Binding[string]
	Kind fb.Binding[string]
}

type generatorStructuredCollectionAPI struct{}

func (*generatorStructuredCollectionAPI) Replace(context.Context, []generatorStructuredCollectionItem) {
}

func (*generatorStructuredCollectionAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Replace": {Request: fb.Body[[]generatorStructuredCollectionItem](
			fb.Elements(fb.ElementObject[generatorStructuredCollectionItem](
				generatorStructuredCollectionItemContract{
					Name: fb.Value[string](fb.Required(), fb.MinLength(1)).Document("Tag name."),
					Kind: fb.Value[string](fb.OneOf("system", "user")).Document("Tag kind."),
				},
			).Document("Tag item.")),
		)},
	}}
}

type generatorCompositeDefaultRequest struct {
	Attributes map[string]any
	Tags       []string
	Pointer    *string
	Named      generatorStringPointer
}

type generatorStringPointer *string

type generatorCompositeDefaultContract struct {
	Attributes fb.Binding[map[string]any]
	Tags       fb.Binding[[]string]
	Pointer    fb.Binding[*string]
	Named      fb.Binding[generatorStringPointer]
}

type generatorCompositeDefaultValue struct {
	Name string
}

type generatorCompositeDefaultAPI struct{}

func (*generatorCompositeDefaultAPI) Create(context.Context, generatorCompositeDefaultRequest) {}

func (*generatorCompositeDefaultAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Create": {Request: fb.BodyObject(generatorCompositeDefaultContract{
			Attributes: fb.Value[map[string]any](fb.Default(map[string]any{
				"nested": map[string]any{"tags": []any{}},
				"typed":  generatorCompositeDefaultValue{Name: "fixture"},
			})),
			Tags:    fb.Value[[]string](fb.Default([]string{})),
			Pointer: fb.Value[*string](fb.Default("fallback")),
			Named:   fb.Value[generatorStringPointer](fb.Default("named")),
		})},
	}}
}

type generatorTypedResponseAPI struct{}

func (generatorTypedResponseAPI) Run() {}

func (generatorTypedResponseAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Run": {Responses: map[int]fb.ResponseDocumentation{
			400: {GoType: reflect.TypeFor[domain.PageSize]()},
		}},
	}}
}

type generatorCoordinates struct {
	Latitude  float64
	Longitude float64
}

type generatorPagination struct {
	PageSize domain.PageSize
}

type generatorFilters struct {
	Pagination generatorPagination
	Location   *generatorCoordinates
}

type generatorStructuralRequest struct {
	ID      string
	Filters *generatorFilters
	TraceID string
	Payload generatorRequest
	Subject string
}

type generatorCoordinateContract struct {
	Latitude  fb.Binding[float64]
	Longitude fb.Binding[float64]
}

type generatorPaginationContract struct {
	PageSize fb.Binding[domain.PageSize]
}

type generatorFilterContract struct {
	Pagination generatorPaginationContract
	Location   *generatorCoordinateContract
}

type generatorStructuralContract struct {
	ID      fb.Binding[string]
	Filters *generatorFilterContract
	TraceID fb.Binding[string]
	Payload fb.Binding[generatorRequest]
	Subject fb.Binding[string]
}

type generatorResponseContract struct {
	ID fb.ResponseValueBinding[string]
}

type generatorStructuralAPI struct{}

func (*generatorStructuralAPI) Search(context.Context, generatorStructuralRequest) generatorResponse {
	return generatorResponse{}
}

func (*generatorStructuralAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Search": {
			Request: generatorStructuralContract{
				ID: fb.Path[string]("id", fb.MinLength(1)),
				Filters: &generatorFilterContract{
					Pagination: generatorPaginationContract{
						PageSize: fb.Query[domain.PageSize](
							"page_size",
							fb.Default(domain.PageSize(20)),
							fb.Min(domain.PageSize(1)),
							fb.Max(domain.PageSize(100)),
						).Document(
							"Maximum results per page",
							fb.Example(domain.PageSize(25)),
							fb.Deprecated(),
						),
					},
					Location: &generatorCoordinateContract{
						Latitude:  fb.Query[float64]("lat", fb.Min(float64(-90)), fb.Max(float64(90))),
						Longitude: fb.Query[float64]("lon", fb.Min(float64(-180)), fb.Max(float64(180))),
					},
				},
				TraceID: fb.Header[string]("X-Trace-ID", fb.Required(), fb.WithFormat("uuid")),
				Payload: fb.Body[generatorRequest](fb.Required()).Document(
					"Search payload",
					fb.Example(generatorRequest{Name: "fixture"}),
				),
				Subject: fb.Subject(),
			},
			Response: generatorResponseContract{
				ID: fb.ResponseValue[string](fb.Required(), fb.MinLength(1)).Document(
					"Matched fixture identifier",
					fb.Example("fixture-1"),
				),
			},
		},
	}}
}

func (*generatorStructuralAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Search": {Route: "/fixtures/:id/search"},
	}}
}

func (*generatorFixtureAPI) Create(context.Context, *generatorRequest) *generatorResponse {
	return &generatorResponse{}
}

func TestGeneratePreservesValueReceiverProvider(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{generatorValueAPI{}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if source := string(contents); !strings.Contains(source, "func (api generatorValueAPI) KobaCompiledAPI()") {
		t.Fatalf("generated provider does not preserve the value receiver:\n%s", source)
	}
}

func TestGenerateDoesNotImportReflectWhenContractHasNoTypes(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{generatorVoidAPI{}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if source := string(contents); strings.Contains(source, `reflect "reflect"`) {
		t.Fatalf("generated void-only provider contains an unused reflect import:\n%s", source)
	}
}

func TestGeneratePreservesWebSocketTransportTypes(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorWebSocketAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`anypb "google.golang.org/protobuf/types/known/anypb"`,
		`typepb "google.golang.org/protobuf/types/known/typepb"`,
		`Transport: fb.WebSocket(fb.Socket[*typepb.Type, *anypb.Any]())`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated WebSocket plan does not contain %q:\n%s", want, source)
		}
	}
}

func TestGeneratePreservesDirectionalSocketMessageContracts(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorDocumentedWebSocketAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`Transport: fb.WebSocket(fb.SocketContract{ClientToServerType: reflect.TypeFor[*typepb.Type](), ServerToClientType: reflect.TypeFor[*typepb.Enum]()`,
		`ClientToServer: &fb.SocketMessageContract{Description: "Client message.", Bindings: []fb.ResponseBinding{`,
		`Documentation: &fb.BindingDocumentation{Description: "Client name.", Example: "example.Client"}`,
		`ServerToClient: &fb.SocketMessageContract{Description: "Server message.", Bindings: []fb.ResponseBinding{`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated directional socket plan does not contain %q:\n%s", want, source)
		}
	}
	if strings.Contains(source, "generatorDocumentedSocketContract") {
		t.Fatalf("generated plan retained the authoring companion:\n%s", source)
	}
}

func TestGeneratePreservesScalarCollectionElementContract(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorCollectionAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`UniqueItems: true`,
		`Items: &fb.CollectionItemContract{Input: fb.InputContract{GoType: reflect.TypeFor[string](), Format: "uuid"}, Documentation: &fb.BindingDocumentation{Description: "Stable collection identifier.", Example: "49a8511f-561a-43f3-b04b-309411895786"}}`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated collection plan does not contain %q:\n%s", want, source)
		}
	}
}

func TestGeneratePreservesStructuredCollectionElementContract(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorStructuredCollectionAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`Items: &fb.CollectionItemContract{Input: fb.InputContract{GoType: reflect.TypeFor[generatorStructuredCollectionItem]()}, Documentation: &fb.BindingDocumentation{Description: "Tag item."}, Bindings: []fb.ResponseBinding{`,
		`FieldPath: []string{"Name"}, FieldIndex: []int{0}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Required: true, MinLength:`,
		`Documentation: &fb.BindingDocumentation{Description: "Tag name."}`,
		`FieldPath: []string{"Kind"}, FieldIndex: []int{1}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Enum: []any{"system", "user"}}`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated structured collection plan does not contain %q:\n%s", want, source)
		}
	}
	if strings.Contains(source, "generatorStructuredCollectionItemContract") {
		t.Fatalf("generated plan retained the authoring companion:\n%s", source)
	}
}

func TestGeneratePreservesCompositeDefaults(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorCompositeDefaultAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`Default: map[string]any{"nested": map[string]any{"tags": []any{}}, "typed": generatorCompositeDefaultValue{Name: "fixture"}}, HasDefault: true`,
		`Default: []string{}, HasDefault: true`,
		`Default: func() *string { var value string = "fallback"; return &value }(), HasDefault: true`,
		`Default: func() generatorStringPointer { var value string = "named"; return generatorStringPointer(&value) }(), HasDefault: true`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated composite defaults do not contain %q:\n%s", want, source)
		}
	}
}

func TestGenerateImportsTypedResponseDependenciesBeforeWritingSource(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{generatorTypedResponseAPI{}},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	for _, want := range []string{
		`reflect "reflect"`,
		`domain "github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"`,
		`GoType: reflect.TypeFor[domain.PageSize]()`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated source does not contain %q:\n%s", want, source)
		}
	}
}

func (*generatorFixtureAPI) Get(context.Context) generatorResponse {
	return generatorResponse{}
}

func (*generatorFixtureAPI) Delete() {}

func (*generatorFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Name: "generator", Description: "Generator fixture.", Operations: map[string]fb.OperationDocumentation{
		"Create": {
			OperationID: "createGeneratorFixture",
			Summary:     "Create fixture",
			Tags:        []string{"fixtures"},
			Parameters: []fb.ParameterDocumentation{{
				Name: "mode", In: fb.ParameterInQuery, GoType: reflect.TypeFor[string](),
				Enum: []any{"safe", "fast"}, Example: "safe",
			}},
			Responses: map[int]fb.ResponseDocumentation{400: {
				Description: "Invalid fixture",
				GoType:      reflect.TypeFor[generatorResponse](),
				Headers: map[string]fb.HeaderDocumentation{
					"X-Request-ID": {
						Description: "Request correlation ID",
						Required:    true,
						Format:      "uuid",
						Pattern:     "^[a-f0-9-]+$",
						Enum:        []string{"abc-123"},
						Example:     "abc-123",
					},
				},
			}},
			Security: []fb.SecurityRequirement{{"tokenAuth"}},
		},
		"Get": {
			OperationID: "getGeneratorFixture",
			Security:    []fb.SecurityRequirement{{"tokenAuth"}, {}},
		},
		"Delete": {OperationID: "deleteGeneratorFixture"},
	}}
}

func (*generatorFixtureAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Create": {
			Authorization: fb.AuthorizationContract{
				RequiredScopes: []string{"create:fixtures"},
			},
		},
	}}
}

func TestGenerateEmitsTypedInvokersBehindBuildTag(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorFixtureAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	checks := []string{
		"//go:build koba_static",
		"import (\n\treflect \"reflect\"\n\n\tfb \"github.com/roxiegrillo/koba/v2\"",
		"func (api *generatorFixtureAPI) KobaCompiledAPI() fb.CompiledAPI",
		`Description: fb.APIDescription{Name: "generator", Description: "Generator fixture."`,
		`InputType: reflect.TypeFor[*generatorRequest]()`,
		`OutputType: reflect.TypeFor[*generatorResponse]()`,
		`Enum: []any{"safe", "fast"}`,
		`GoType: reflect.TypeFor[generatorResponse]()`,
		`Headers: map[string]fb.HeaderDocumentation{"X-Request-ID": {Description: "Request correlation ID", Required: true, Format: "uuid", Pattern: "^[a-f0-9-]+$", Enum: []string{"abc-123"}, Example: "abc-123"}}`,
		`Authorization: fb.AuthorizationContract{RequiredScopes: []string{"create:fixtures"}}`,
		`Security: []fb.SecurityRequirement{{"tokenAuth"}, {}}`,
		"var request *generatorRequest",
		"if invocation.HasInput {",
		"invocation.Decode(&request)",
		"request = new(generatorRequest)",
		"invocation.Decode(request)",
		"api.Create(invocation.Context, request)",
		"if value == nil {",
		"Value: nil, HasValue: true",
		"Value: *value",
		"value := api.Get(invocation.Context)",
		"api.Delete()",
	}
	for _, check := range checks {
		if !strings.Contains(source, check) {
			t.Fatalf("generated source does not contain %q:\n%s", check, source)
		}
	}
	if strings.Contains(source, "DescribeReflectedAPI") {
		t.Fatalf("generated static plan still reflects its description at runtime:\n%s", source)
	}
}

func TestGenerateSerializesResolvedDeepRequestBindings(t *testing.T) {
	contents, err := Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{(*generatorStructuralAPI)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	source := string(contents)
	checks := []string{
		`domain "github.com/roxiegrillo/koba/v2/internal/staticfixture/domain"`,
		`RequestBindings: []fb.RequestBinding{`,
		`{Source: fb.BindingSource("path"), Name: "id", FieldPath: []string{"ID"}, FieldIndex: []int{0}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Required: true, MinLength: func() *uint64 { value := uint64(1); return &value }()}}`,
		`{Source: fb.BindingSource("query"), Name: "page_size", FieldPath: []string{"Filters", "Pagination", "PageSize"}, FieldIndex: []int{1, 0, 0}, Input: fb.InputContract{GoType: reflect.TypeFor[domain.PageSize](), Default: domain.PageSize(int(20)), HasDefault: true, Minimum: domain.PageSize(int(1)), Maximum: domain.PageSize(int(100))}, Documentation: &fb.BindingDocumentation{Description: "Maximum results per page", Deprecated: true, Example: domain.PageSize(int(25))}}`,
		`{Source: fb.BindingSource("query"), Name: "lat", FieldPath: []string{"Filters", "Location", "Latitude"}, FieldIndex: []int{1, 1, 0}, Input: fb.InputContract{GoType: reflect.TypeFor[float64](), Minimum: float64(-90), Maximum: float64(90)}}`,
		`{Source: fb.BindingSource("header"), Name: "X-Trace-ID", FieldPath: []string{"TraceID"}, FieldIndex: []int{2}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Required: true, Format: "uuid"}}`,
		`{Source: fb.BindingSource("body"), Name: "", FieldPath: []string{"Payload"}, FieldIndex: []int{3}, Input: fb.InputContract{GoType: reflect.TypeFor[generatorRequest](), Required: true}, Documentation: &fb.BindingDocumentation{Description: "Search payload", Example: generatorRequest{Name: "fixture"}}}`,
		`{Source: fb.BindingSource("subject"), Name: "", FieldPath: []string{"Subject"}, FieldIndex: []int{4}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Required: true, MinLength: func() *uint64 { value := uint64(1); return &value }()}}`,
		`ResponseBindings: []fb.ResponseBinding{{FieldPath: []string{"ID"}, FieldIndex: []int{0}, Input: fb.InputContract{GoType: reflect.TypeFor[string](), Required: true, MinLength: func() *uint64 { value := uint64(1); return &value }()}, Documentation: &fb.BindingDocumentation{Description: "Matched fixture identifier", Example: "fixture-1"}}}`,
		`Documentation: fb.OperationDocumentation{Route: "/fixtures/:id/search"`,
		`invocation.Decode(&request)`,
	}
	for _, check := range checks {
		if !strings.Contains(source, check) {
			t.Fatalf("generated source does not contain %q:\n%s", check, source)
		}
	}
	for _, forbidden := range []string{
		"generatorStructuralContract",
		"generatorFilterContract",
		"generatorCoordinateContract",
		"generatorPaginationContract",
		"Contract: fb.OperationContract",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generated source serializes raw request descriptor %q:\n%s", forbidden, source)
		}
	}
}

func TestGenerateRejectsInvalidConfiguration(t *testing.T) {
	_, err := Generate(Config{})
	if err == nil || !strings.Contains(err.Error(), "package name is required") {
		t.Fatalf("Generate() error = %v, want missing-package error", err)
	}
	_, err = Generate(Config{
		PackageName: "staticgen",
		PackagePath: "github.com/roxiegrillo/koba/v2/staticgen",
		APIs:        []any{nil},
	})
	if err == nil || !strings.Contains(err.Error(), "API 1 is nil") {
		t.Fatalf("Generate() error = %v, want nil-API error", err)
	}
}

func TestConfigPublicShape(t *testing.T) {
	configType := reflect.TypeFor[Config]()
	if got, want := configType.PkgPath(), "github.com/roxiegrillo/koba/v2/staticgen"; got != want {
		t.Fatalf("Config package path = %q, want %q", got, want)
	}
	if got, want := configType.Name(), "Config"; got != want {
		t.Fatalf("Config name = %q, want %q", got, want)
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "PackageName", typeOf: reflect.TypeFor[string]()},
		{name: "PackagePath", typeOf: reflect.TypeFor[string]()},
		{name: "BuildTag", typeOf: reflect.TypeFor[string]()},
		{name: "APIs", typeOf: reflect.TypeFor[[]any]()},
	}
	if got, want := configType.NumField(), len(wantFields); got != want {
		t.Fatalf("Config field count = %d, want %d", got, want)
	}
	for index, want := range wantFields {
		field := configType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Errorf("Config field %d = %s %s, want %s %s", index, field.Name, field.Type, want.name, want.typeOf)
		}
	}
}

func TestGenerateMatchesCommittedStaticFixtureByteForByte(t *testing.T) {
	want, err := os.ReadFile("../internal/staticfixture/zz_koba.gen.go")
	if err != nil {
		t.Fatalf("read committed static fixture: %v", err)
	}
	got, err := Generate(Config{
		PackageName: "staticfixture",
		PackagePath: "github.com/roxiegrillo/koba/v2/internal/staticfixture",
		APIs:        []any{(*staticfixture.API)(nil)},
	})
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generated output differs from committed static fixture")
	}
}
