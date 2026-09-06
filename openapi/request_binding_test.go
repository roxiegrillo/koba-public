package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

type projectedRequestBindingsInput struct {
	Filters struct {
		PageSize int `json:"page_size"`
	} `json:"filters"`
	Identity struct {
		ID      string `json:"id"`
		TraceID string `json:"trace_id"`
		Session string `json:"session"`
	} `json:"identity"`
}

type projectedRequestBody struct {
	Name    string                     `json:"name"`
	Details projectedRequestBodyDetail `json:"details"`
}

type projectedRequestBodyDetail struct {
	Note string `json:"note"`
}

type projectedMixedRequest struct {
	ID      string               `json:"id"`
	Payload projectedRequestBody `json:"payload"`
}

type projectedSubjectRequest struct {
	Subject string
}

type projectedObjectRuleBody struct {
	Name      *string                     `json:"name,omitempty"`
	Latitude  *float64                    `json:"lat,omitempty"`
	Longitude *float64                    `json:"lon,omitempty"`
	Tags      []string                    `json:"tags,omitempty"`
	Details   *projectedRequestBodyDetail `json:"details,omitempty"`
}

type projectedCollectionDetails struct {
	Code string `json:"code"`
}

type projectedCollectionItem struct {
	Name    string                     `json:"name"`
	Kind    string                     `json:"kind"`
	Details projectedCollectionDetails `json:"details"`
}

type projectedCollectionDetailsContract struct {
	Code fb.Binding[string]
}

type projectedCollectionItemContract struct {
	Name    fb.Binding[string]
	Kind    fb.Binding[string]
	Details projectedCollectionDetailsContract
}

type projectedStructuredCollectionAPI struct{}

func (*projectedStructuredCollectionAPI) Replace(context.Context, []projectedCollectionItem) {}

func (*projectedStructuredCollectionAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Replace": {Route: "/tags", Method: http.MethodPut, OperationID: "replaceTags"},
	}}
}

func (*projectedStructuredCollectionAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Replace": {Request: fb.Body[[]projectedCollectionItem](
			fb.Required(),
			fb.UniqueItems(),
			fb.Elements(fb.ElementObject[projectedCollectionItem](projectedCollectionItemContract{
				Name: fb.Value[string](fb.Required(), fb.MinLength(1)).Document(
					"Tag name.", fb.Example("featured"),
				),
				Kind: fb.Value[string](fb.Required(), fb.OneOf("system", "user")).Document(
					"Tag kind.",
				),
				Details: projectedCollectionDetailsContract{
					Code: fb.Value[string](fb.Required(), fb.WithFormat("slug")).Document(
						"Nested tag code.",
					),
				},
			}).Document("Tag attached to an item.")),
		)},
	}}
}

func TestBuildTreatsSubjectAsAuthenticationWithoutExposingRequestInput(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "WhoAmI",
		OperationID: "whoAmI",
		Route:       "/whoami",
		HTTPMethod:  http.MethodGet,
		InputType:   reflect.TypeFor[projectedSubjectRequest](),
		RequestBindings: []fb.RequestBinding{{
			Source:     fb.BindingSourceSubject,
			FieldPath:  []string{"Subject"},
			FieldIndex: []int{0},
			Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1)),
		}},
	}}}
	document, err := Build(Config{
		Title:   "Identity API",
		Version: "1",
		SecuritySchemes: []SecurityScheme{{
			Name:   "bearerAuth",
			Type:   SecurityTypeHTTP,
			Scheme: "bearer",
		}},
		DefaultSecurity: []SecurityRequirement{{"bearerAuth"}},
	}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/whoami").Get
	if len(operation.Parameters) != 0 || operation.RequestBody != nil {
		t.Fatalf("subject leaked into OpenAPI inputs: parameters=%#v body=%#v", operation.Parameters, operation.RequestBody)
	}
	if operation.Security == nil || len(*operation.Security) != 1 {
		t.Fatalf("subject operation security = %#v, want one requirement", operation.Security)
	}
	if response := operation.Responses.Value("401"); response == nil || response.Value == nil {
		t.Fatal("subject operation is missing its 401 response")
	}
	if response := operation.Responses.Value("400"); response != nil {
		t.Fatalf("subject-only operation unexpectedly documents 400: %#v", response)
	}
	if response := operation.Responses.Value("403"); response != nil {
		t.Fatalf("subject-only operation unexpectedly documents 403: %#v", response)
	}
}

func TestBuildProjectsNestedRequestBindingsAsParametersWithoutRequestBody(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "List",
		OperationID: "listThings",
		Route:       "/things/:id",
		HTTPMethod:  http.MethodGet,
		InputType:   reflect.TypeFor[projectedRequestBindingsInput](),
		RequestBindings: []fb.RequestBinding{
			{
				Source:     fb.BindingSourceQuery,
				Name:       "page_size",
				FieldPath:  []string{"Filters", "PageSize"},
				FieldIndex: []int{0, 0},
				Input:      fb.InputOf[int](fb.Default(20), fb.Min(1), fb.Max(100)),
				Documentation: &fb.BindingDocumentation{
					Description: "Maximum number of things to return",
					Deprecated:  true,
					Example:     42,
				},
			},
			{
				Source:     fb.BindingSourcePath,
				Name:       "id",
				FieldPath:  []string{"Identity", "ID"},
				FieldIndex: []int{1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1)),
			},
			{
				Source:     fb.BindingSourceHeader,
				Name:       "X-Trace-ID",
				FieldPath:  []string{"Identity", "TraceID"},
				FieldIndex: []int{1, 1},
				Input:      fb.InputOf[string](fb.Required(), fb.WithFormat("uuid")),
			},
			{
				Source:     fb.BindingSourceCookie,
				Name:       "session",
				FieldPath:  []string{"Identity", "Session"},
				FieldIndex: []int{1, 2},
				Input:      fb.InputOf[string](fb.MaxLength(64)),
			},
		},
		Documentation: fb.OperationDocumentation{Parameters: []fb.ParameterDocumentation{
			{
				Name:        "x-trace-id",
				In:          fb.ParameterInHeader,
				Description: "Request correlation ID",
				Example:     "33197c6a-c56a-4567-a850-aab93c0d6c61",
			},
		}},
	}}}

	document, err := Build(Config{Title: "Things", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/things/{id}").Get
	if operation.RequestBody != nil {
		t.Fatalf("query-only request generated a request body: %#v", operation.RequestBody)
	}

	parameters := make(map[string]*openapi3.Parameter, len(operation.Parameters))
	for _, reference := range operation.Parameters {
		parameters[reference.Value.Name] = reference.Value
	}
	if len(parameters) != 4 {
		t.Fatalf("parameters = %#v, want four request bindings", parameters)
	}
	pageSize := parameters["page_size"]
	if pageSize == nil || pageSize.In != openapi3.ParameterInQuery {
		t.Fatalf("page_size parameter = %#v", pageSize)
	}
	if pageSize.Description != "Maximum number of things to return" || !pageSize.Deprecated || pageSize.Example != 42 {
		t.Errorf("page_size documentation = %#v", pageSize)
	}
	if pageSize.Required {
		t.Error("defaulted page_size binding was rendered as required")
	}
	if schema := pageSize.Schema.Value; schema.Default != 20 || schema.Min == nil || *schema.Min != 1 || schema.Max == nil || *schema.Max != 100 {
		t.Errorf("page_size schema = %#v", schema)
	}
	assertParameter(t, parameters["id"], openapi3.ParameterInPath, true)
	assertParameter(t, parameters["X-Trace-ID"], openapi3.ParameterInHeader, true)
	assertParameter(t, parameters["session"], openapi3.ParameterInCookie, false)
	traceID := parameters["X-Trace-ID"]
	if traceID.Description != "Request correlation ID" || traceID.Example != "33197c6a-c56a-4567-a850-aab93c0d6c61" {
		t.Errorf("header documentation was not matched case-insensitively: %#v", traceID)
	}
	if traceID.Schema.Value.Format != "uuid" {
		t.Errorf("trace ID format = %q, want uuid", traceID.Schema.Value.Format)
	}
}

func TestBuildUsesBodyBindingTargetTypeForMixedRequest(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Update",
		OperationID: "updateThing",
		Route:       "/things/:id",
		HTTPMethod:  http.MethodPatch,
		InputType:   reflect.TypeFor[projectedMixedRequest](),
		RequestBindings: []fb.RequestBinding{
			{
				Source:     fb.BindingSourcePath,
				Name:       "id",
				FieldPath:  []string{"ID"},
				FieldIndex: []int{0},
				Input:      fb.InputOf[string](fb.Required()),
			},
			{
				Source:     fb.BindingSourceBody,
				FieldPath:  []string{"Payload"},
				FieldIndex: []int{1},
				Input:      fb.InputOf[projectedRequestBody](fb.Required()),
				Documentation: &fb.BindingDocumentation{
					Description: "Fields to update",
				},
			},
			{
				Source:     fb.BindingSourceValue,
				FieldPath:  []string{"Payload", "Name"},
				FieldIndex: []int{1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(3), fb.MaxLength(40), fb.Matches(`^[A-Z]`)),
				Documentation: &fb.BindingDocumentation{
					Description: "Display name.",
					Deprecated:  true,
					Example:     "Widget",
				},
			},
			{
				Source:     fb.BindingSourceValue,
				FieldPath:  []string{"Payload", "Details", "Note"},
				FieldIndex: []int{1, 1, 0},
				Input:      fb.InputOf[string](fb.Required(), fb.MinLength(1)),
				Documentation: &fb.BindingDocumentation{
					Description: "Internal note.",
				},
			},
		},
	}}}

	document, err := Build(Config{Title: "Things", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/things/{id}").Patch
	if len(operation.Parameters) != 1 || operation.Parameters[0].Value.Name != "id" {
		t.Fatalf("parameters = %#v, want only the path binding", operation.Parameters)
	}
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		t.Fatal("body binding did not generate a request body")
	}
	if !operation.RequestBody.Value.Required {
		t.Error("required body binding generated an optional request body")
	}
	if operation.RequestBody.Value.Description != "Fields to update" {
		t.Errorf("request body description = %q", operation.RequestBody.Value.Description)
	}
	media := operation.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatalf("request body media = %#v", media)
	}
	if wrongRef := "#/components/schemas/" + componentName(reflect.TypeFor[projectedMixedRequest]()); media.Schema.Ref == wrongRef {
		t.Errorf("request body schema used complete operation input type %q", wrongRef)
	}
	bodySchema := media.Schema.Value
	if bodySchema == nil {
		t.Fatalf("request body schema = %#v, want request-local schema", media.Schema)
	}
	if !containsString(bodySchema.Required, "name") || !containsString(bodySchema.Required, "details") {
		t.Errorf("body required fields = %v, want name and details", bodySchema.Required)
	}
	if bodySchema.Description != "Fields to update" {
		t.Errorf("body schema description = %q", bodySchema.Description)
	}
	name := bodySchema.Properties["name"]
	if name == nil || name.Value == nil || name.Value.MinLength != 3 || name.Value.MaxLength == nil || *name.Value.MaxLength != 40 || name.Value.Pattern != `^[A-Z]` {
		t.Errorf("body name contract = %#v", name)
	}
	if name.Value.Description != "Display name." || !name.Value.Deprecated || name.Value.Example != "Widget" {
		t.Errorf("body name documentation = %#v", name.Value)
	}
	details := resolvedSchema(bodySchema.Properties["details"], document.Components.Schemas)
	if details == nil || !containsString(details.Required, "note") {
		t.Fatalf("nested body contract = %#v, want required note", details)
	}
	note := details.Properties["note"]
	if note == nil || note.Value == nil || note.Value.MinLength != 1 {
		t.Errorf("nested note contract = %#v", note)
	}
	if note.Value.Description != "Internal note." {
		t.Errorf("nested note documentation = %#v", note.Value)
	}
}

func TestBuildProjectsRootBodyConstraints(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Echo",
		OperationID: "echo",
		Route:       "/echo",
		HTTPMethod:  http.MethodPost,
		InputType:   reflect.TypeFor[string](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input: fb.InputOf[string](
				fb.Default("hello"),
				fb.MinLength(3),
				fb.MaxLength(20),
				fb.Matches(`^[a-z]+$`),
			),
		}},
	}}}

	document, err := Build(Config{Title: "Echo", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/echo").Post.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("body schema = %#v", media)
	}
	schema := media.Schema.Value
	if schema.Default != "hello" || schema.MinLength != 3 || schema.MaxLength == nil || *schema.MaxLength != 20 || schema.Pattern != `^[a-z]+$` {
		t.Errorf("body contract schema = %#v", schema)
	}
}

func TestBuildProjectsRootCollectionCardinality(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Replace",
		OperationID: "replaceLabels",
		Route:       "/labels",
		HTTPMethod:  http.MethodPut,
		InputType:   reflect.TypeFor[[]string](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input:  fb.InputOf[[]string](fb.Required(), fb.MinItems(1), fb.MaxItems(2)),
		}},
	}}}

	document, err := Build(Config{Title: "Labels", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/labels").Put.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("body schema = %#v", media)
	}
	schema := media.Schema.Value
	if schema.MinItems != 1 || schema.MaxItems == nil || *schema.MaxItems != 2 {
		t.Errorf("body item bounds = %d..%v, want 1..2", schema.MinItems, schema.MaxItems)
	}
}

func TestBuildProjectsScalarCollectionElementContract(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Replace",
		OperationID: "replaceAgents",
		Route:       "/agents",
		HTTPMethod:  http.MethodPut,
		InputType:   reflect.TypeFor[[]string](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input: fb.InputOf[[]string](
				fb.Required(),
				fb.MaxItems(64),
				fb.UniqueItems(),
				fb.Elements(fb.Element[string](
					fb.MinLength(1),
					fb.WithFormat("uuid"),
				).Document("Stable UUID of an existing domain agent.")),
			),
		}},
	}}}

	document, err := Build(Config{Title: "Agents", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/agents").Put.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("body schema = %#v", media)
	}
	collection := media.Schema.Value
	if !collection.UniqueItems || collection.MaxItems == nil || *collection.MaxItems != 64 {
		t.Fatalf("collection schema = %#v", collection)
	}
	if collection.Items == nil || collection.Items.Value == nil {
		t.Fatalf("collection items = %#v", collection.Items)
	}
	item := collection.Items.Value
	if item.Type == nil || !item.Type.Is("string") || item.Format != "uuid" || item.MinLength != 1 ||
		item.Description != "Stable UUID of an existing domain agent." {
		t.Fatalf("collection item schema = %#v", item)
	}
}

func TestBuildProjectsStructuredCollectionElementContract(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*projectedStructuredCollectionAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}

	document, err := Build(Config{Title: "Tags", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	collection := document.Paths.Find("/tags").Put.RequestBody.Value.Content["application/json"].Schema.Value
	if collection == nil || !collection.UniqueItems || collection.Items == nil || collection.Items.Value == nil {
		t.Fatalf("collection schema = %#v", collection)
	}
	item := collection.Items.Value
	if item.Description != "Tag attached to an item." ||
		!containsString(item.Required, "name") ||
		!containsString(item.Required, "kind") ||
		!containsString(item.Required, "details") {
		t.Fatalf("item schema = %#v", item)
	}
	name := item.Properties["name"].Value
	if name.Description != "Tag name." || name.MinLength != 1 || name.Example != "featured" {
		t.Fatalf("name schema = %#v", name)
	}
	kind := item.Properties["kind"].Value
	if kind.Description != "Tag kind." || !reflect.DeepEqual(kind.Enum, []any{"system", "user"}) {
		t.Fatalf("kind schema = %#v", kind)
	}
	details := resolvedSchema(item.Properties["details"], document.Components.Schemas)
	if details == nil || !containsString(details.Required, "code") ||
		details.Properties["code"].Value.Format != "slug" ||
		details.Properties["code"].Value.Description != "Nested tag code." {
		t.Fatalf("nested details schema = %#v", details)
	}
}

func TestBuildProjectsCompositeDefaults(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Replace",
		OperationID: "replaceAttributes",
		Route:       "/attributes",
		HTTPMethod:  http.MethodPut,
		InputType:   reflect.TypeFor[map[string]any](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input: fb.InputOf[map[string]any](fb.Default(map[string]any{
				"nested": map[string]any{"tags": []any{}},
			})),
		}},
	}}}
	document, err := Build(Config{Title: "Attributes", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	schema := document.Paths.Find("/attributes").Put.RequestBody.Value.Content["application/json"].Schema.Value
	want := map[string]any{"nested": map[string]any{"tags": []any{}}}
	if !reflect.DeepEqual(schema.Default, want) {
		t.Fatalf("composite default = %#v, want %#v", schema.Default, want)
	}
}

type projectedTypedCompositeDefault struct {
	ID     uint64            `json:"id"`
	Labels map[string]string `json:"labels"`
}

func TestBuildProjectsTypedCompositeDefaultsWithoutLosingIntegerPrecision(t *testing.T) {
	value := projectedTypedCompositeDefault{
		ID:     ^uint64(0),
		Labels: map[string]string{"mode": "safe"},
	}
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Replace",
		OperationID: "replaceTypedAttributes",
		Route:       "/typed-attributes",
		HTTPMethod:  http.MethodPut,
		InputType:   reflect.TypeFor[projectedTypedCompositeDefault](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input:  fb.InputOf[projectedTypedCompositeDefault](fb.Default(value)),
		}},
	}}}
	document, err := Build(Config{Title: "Attributes", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	schema := document.Paths.Find("/typed-attributes").Put.RequestBody.Value.Content["application/json"].Schema.Value
	want := map[string]any{
		"id":     json.Number("18446744073709551615"),
		"labels": map[string]any{"mode": "safe"},
	}
	if !reflect.DeepEqual(schema.Default, want) {
		t.Fatalf("typed composite default = %#v, want %#v", schema.Default, want)
	}
}

func TestBuildProjectsBodyObjectPresenceRules(t *testing.T) {
	description := fb.APIDescription{Operations: []fb.OperationDescription{{
		MethodName:  "Update",
		OperationID: "updateThing",
		Route:       "/things",
		HTTPMethod:  http.MethodPatch,
		InputType:   reflect.TypeFor[projectedObjectRuleBody](),
		RequestBindings: []fb.RequestBinding{{
			Source: fb.BindingSourceBody,
			Input:  fb.InputOf[projectedObjectRuleBody](fb.Required()),
			ObjectRules: []fb.ObjectRule{
				fb.AtLeastOne("Name", "Details.Note", "Latitude", "Longitude"),
				fb.AllOrNone("Latitude", "Longitude"),
				fb.SameNullState("Latitude", "Longitude"),
			},
		}, {
			Source:     fb.BindingSourceValue,
			FieldPath:  []string{"Name"},
			FieldIndex: []int{0},
			Input:      fb.InputOf[*string](),
		}, {
			Source:     fb.BindingSourceValue,
			FieldPath:  []string{"Latitude"},
			FieldIndex: []int{1},
			Input:      fb.InputOf[*float64](fb.Nullable(), fb.Min(-90.0), fb.Max(90.0)),
		}, {
			Source:     fb.BindingSourceValue,
			FieldPath:  []string{"Longitude"},
			FieldIndex: []int{2},
			Input:      fb.InputOf[*float64](fb.Nullable(), fb.Min(-180.0), fb.Max(180.0)),
		}, {
			Source:     fb.BindingSourceValue,
			FieldPath:  []string{"Tags"},
			FieldIndex: []int{3},
			Input:      fb.InputOf[[]string](fb.MaxItems(20)),
		}},
	}}}

	document, err := Build(Config{Title: "Things", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	media := document.Paths.Find("/things").Patch.RequestBody.Value.Content["application/json"]
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatalf("body schema = %#v", media)
	}
	schema := media.Schema.Value
	if schema.Properties["name"].Value.Nullable {
		t.Error("non-nullable pointer value remained nullable in OpenAPI")
	}
	if !schema.Properties["lat"].Value.Nullable || !schema.Properties["lon"].Value.Nullable {
		t.Error("nullable coordinate values were not nullable in OpenAPI")
	}
	tags := schema.Properties["tags"]
	if tags == nil || tags.Value == nil || tags.Value.MaxItems == nil || *tags.Value.MaxItems != 20 {
		t.Errorf("tags cardinality = %#v, want maxItems 20", tags)
	}
	if len(schema.AllOf) != 3 {
		t.Fatalf("body allOf = %#v, want one entry per object rule", schema.AllOf)
	}

	atLeastOne := schema.AllOf[0].Value
	if atLeastOne == nil || len(atLeastOne.AnyOf) != 4 {
		t.Fatalf("at-least-one schema = %#v, want four alternatives", atLeastOne)
	}
	if got := atLeastOne.AnyOf[0].Value.Required; !reflect.DeepEqual(got, []string{"name"}) {
		t.Errorf("name presence = %v, want [name]", got)
	}
	nested := atLeastOne.AnyOf[1].Value
	if !reflect.DeepEqual(nested.Required, []string{"details"}) ||
		nested.Properties["details"] == nil ||
		!reflect.DeepEqual(nested.Properties["details"].Value.Required, []string{"note"}) {
		t.Errorf("nested presence schema = %#v", nested)
	}

	allOrNone := schema.AllOf[1].Value
	if allOrNone == nil || len(allOrNone.OneOf) != 2 {
		t.Fatalf("all-or-none schema = %#v, want all/none alternatives", allOrNone)
	}
	all := allOrNone.OneOf[0].Value
	if all == nil || len(all.AllOf) != 2 ||
		!reflect.DeepEqual(all.AllOf[0].Value.Required, []string{"lat"}) ||
		!reflect.DeepEqual(all.AllOf[1].Value.Required, []string{"lon"}) {
		t.Errorf("all-present schema = %#v", all)
	}
	none := allOrNone.OneOf[1].Value
	if none == nil || none.Not == nil || none.Not.Value == nil || len(none.Not.Value.AnyOf) != 2 {
		t.Errorf("none-present schema = %#v", none)
	}

	sameNullState := schema.AllOf[2].Value
	if sameNullState == nil || len(sameNullState.AnyOf) != 2 {
		t.Fatalf("same-null-state schema = %#v, want two alternatives", sameNullState)
	}
	for index, alternative := range sameNullState.AnyOf {
		if alternative == nil || alternative.Value == nil || alternative.Value.Not == nil ||
			alternative.Value.Not.Value == nil || len(alternative.Value.Not.Value.AnyOf) != 2 {
			t.Errorf("same-null-state alternative %d = %#v", index, alternative)
		}
	}
	nonNullLatitude := sameNullState.AnyOf[1].Value.Not.Value.AnyOf[0].Value.Properties["lat"]
	if nonNullLatitude == nil || nonNullLatitude.Value == nil || nonNullLatitude.Value.Type == nil ||
		!nonNullLatitude.Value.Type.Is("number") || nonNullLatitude.Value.Nullable {
		t.Fatalf("non-null latitude matcher = %#v, want a non-null number schema", nonNullLatitude)
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal OpenAPI document: %v", err)
	}
	reloaded, err := openapi3.NewLoader().LoadFromData(encoded)
	if err != nil {
		t.Fatalf("reload serialized OpenAPI document: %v", err)
	}
	schema = reloaded.Paths.Find("/things").Patch.RequestBody.Value.Content["application/json"].Schema.Value
	for _, test := range []struct {
		name  string
		value map[string]any
		valid bool
	}{
		{name: "coordinates absent", value: map[string]any{"name": "thing"}, valid: true},
		{name: "both null", value: map[string]any{"lat": nil, "lon": nil}, valid: true},
		{name: "both values", value: map[string]any{"lat": 19.4, "lon": -99.1}, valid: true},
		{name: "mixed null and value", value: map[string]any{"lat": nil, "lon": -99.1}},
	} {
		t.Run("schema "+test.name, func(t *testing.T) {
			err := schema.VisitJSON(test.value)
			if test.valid && err != nil {
				t.Errorf("valid object rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Error("invalid object accepted")
			}
		})
	}
}
