package koba

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	internalauthorization "github.com/roxiegrillo/koba/v2/internal/authorization"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

type constructedLocation struct {
	Latitude float64
}

type constructedOptions struct {
	PageSize int
	Offset   *int
	Location *constructedLocation
}

type constructedDetails struct {
	Note string `json:"note"`
}

type constructedPayload struct {
	Title   string             `json:"title"`
	Details constructedDetails `json:"details"`
}

type constructedRequest struct {
	ID      string
	Options *constructedOptions
	TraceID string
	Session string
	Payload constructedPayload
}

type constructedLocationContract struct {
	Latitude Binding[float64]
}

type constructedOptionsContract struct {
	PageSize Binding[int]
	Offset   Binding[*int]
	Location *constructedLocationContract
}

type constructedDetailsContract struct {
	Note Binding[string]
}

type constructedPayloadContract struct {
	Title   Binding[string]
	Details constructedDetailsContract
}

type constructedRequestContract struct {
	ID      Binding[string]
	Options *constructedOptionsContract
	TraceID Binding[string]
	Session Binding[string]
	Payload BodyObjectBinding[constructedPayloadContract]
}

type requestConstructionAPI struct {
	received *constructedRequest
}

func (*requestConstructionAPI) Routes() map[string]string {
	return map[string]string{"Execute": "/constructed/:id"}
}

func (*requestConstructionAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"Execute": {
			Method: http.MethodPatch,
			Parameters: []ParameterDocumentation{
				{Name: "id", In: ParameterInPath, Description: "Resource identifier"},
				{Name: "page_size", In: ParameterInQuery, Description: "Page size"},
				{Name: "lat", In: ParameterInQuery, Description: "Latitude"},
				{Name: "X-Trace-ID", In: ParameterInHeader, Description: "Trace identifier"},
				{Name: "session", In: ParameterInCookie, Description: "Session identifier"},
			},
			RequestBodyDescription: "Payload constructed independently from transport parameters.",
		},
	}}
}

func (*requestConstructionAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Execute": {
			Request: constructedRequestContract{
				ID: Path[string]("id", MinLength(1)),
				Options: &constructedOptionsContract{
					PageSize: Query[int]("page_size", Default(20), Min(1), Max(100)),
					Offset:   Query[*int]("offset", Min(0), Max(1000)),
					Location: &constructedLocationContract{
						Latitude: Query[float64]("lat", Min(float64(-90)), Max(float64(90))),
					},
				},
				TraceID: Header[string]("X-Trace-ID", Required()),
				Session: Cookie[string]("session"),
				Payload: BodyObject(constructedPayloadContract{
					Title: Value[string](Required(), MinLength(1)),
					Details: constructedDetailsContract{
						Note: Value[string](MaxLength(32)),
					},
				}, Required()),
			},
		},
	}}
}

func (api *requestConstructionAPI) Execute(_ context.Context, request constructedRequest) map[string]any {
	api.received = &request
	return map[string]any{"page_size": request.Options.PageSize}
}

func TestRequestDescriptorConstructsNestedFunctionInput(t *testing.T) {
	api := &requestConstructionAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/constructed/item-1?lat=19.4326&offset=7",
		jsonBody(t, map[string]any{
			"title":   "Example",
			"details": map[string]any{"note": "nested"},
		}),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Trace-ID", "trace-1")
	request.AddCookie(&http.Cookie{Name: "session", Value: "session-1"})
	response := httptest.NewRecorder()
	app.(WithRouter).Router().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if api.received == nil {
		t.Fatal("API function was not invoked")
	}
	if api.received.ID != "item-1" || api.received.Options == nil || api.received.Options.PageSize != 20 {
		t.Fatalf("path/default input = %#v", api.received)
	}
	if api.received.Options.Offset == nil || *api.received.Options.Offset != 7 {
		t.Fatalf("optional pointer query input = %#v", api.received.Options.Offset)
	}
	if api.received.Options.Location == nil || api.received.Options.Location.Latitude != 19.4326 {
		t.Fatalf("nested query input = %#v", api.received.Options.Location)
	}
	if api.received.TraceID != "trace-1" || api.received.Session != "session-1" {
		t.Fatalf("header/cookie input = %#v", api.received)
	}
	if api.received.Payload.Title != "Example" || api.received.Payload.Details.Note != "nested" {
		t.Fatalf("body input = %#v", api.received.Payload)
	}
}

func TestRequestDescriptorRejectsInvalidOrMissingValuesBeforeInvocation(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		body map[string]any
		want string
	}{
		{name: "query maximum", url: "/constructed/item-1?page_size=101", body: map[string]any{"title": "Example"}, want: "less than or equal to 100"},
		{name: "required body value", url: "/constructed/item-1", body: map[string]any{}, want: `input \"Payload.Title\": is required`},
		{name: "null body value", url: "/constructed/item-1", body: map[string]any{"title": nil}, want: `input \"Payload.Title\": must not be null`},
		{name: "null body ancestor", url: "/constructed/item-1", body: map[string]any{"title": "Example", "details": nil}, want: `input \"Payload.Details.Note\": must not be null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &requestConstructionAPI{}
			app := MakeRawApp()
			app.AddApi(api)
			request := httptest.NewRequest(http.MethodPatch, test.url, jsonBody(t, test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Trace-ID", "trace-1")
			response := httptest.NewRecorder()
			app.(WithRouter).Router().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("response = %d %s, want 400 containing %q", response.Code, response.Body.String(), test.want)
			}
			if api.received != nil {
				t.Fatal("API function was invoked for an invalid request")
			}
		})
	}
}

type subjectRequest struct {
	Subject string
}

type subjectRequestContract struct {
	Subject Binding[string]
}

type subjectAPI struct {
	calls   int
	subject string
}

func (*subjectAPI) Routes() map[string]string {
	return map[string]string{"Read": "/subject"}
}

func (*subjectAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"Read": {Method: http.MethodGet},
	}}
}

func (*subjectAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Read": {Request: subjectRequestContract{Subject: Subject()}},
	}}
}

func (api *subjectAPI) Read(_ context.Context, request subjectRequest) map[string]string {
	api.calls++
	api.subject = request.Subject
	return map[string]string{"subject": request.Subject}
}

func TestSubjectBindingInjectsAuthenticatedPrincipalWithoutScopes(t *testing.T) {
	api := &subjectAPI{}
	authenticationCalls := 0
	app := MakeRawAppWithOptions(WithAuthenticationMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		authenticationCalls++
		ctx := appmiddleware.WithPrincipal(request.Context(), appmiddleware.Principal{UserID: "  auth0|user-1  "})
		*request = *request.WithContext(ctx)
	})))
	app.AddApi(api)

	response := httptest.NewRecorder()
	app.(WithRouter).Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/subject", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if authenticationCalls != 1 || api.calls != 1 || api.subject != "auth0|user-1" {
		t.Fatalf("authentication/API/subject = %d/%d/%q, want 1/1/auth0|user-1", authenticationCalls, api.calls, api.subject)
	}
}

func TestSubjectBindingRejectsMissingOrBlankPrincipalAsUnauthorized(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal *appmiddleware.Principal
	}{
		{name: "missing"},
		{name: "blank", principal: &appmiddleware.Principal{UserID: "  "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &subjectAPI{}
			app := MakeRawAppWithOptions(WithAuthenticationMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				if test.principal != nil {
					ctx := appmiddleware.WithPrincipal(request.Context(), *test.principal)
					*request = *request.WithContext(ctx)
				}
			})))
			app.AddApi(api)

			response := httptest.NewRecorder()
			app.(WithRouter).Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/subject", nil))
			if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("response = %d/%q %s, want 401/Bearer", response.Code, response.Header().Get("WWW-Authenticate"), response.Body.String())
			}
			if api.calls != 0 {
				t.Fatalf("API function invoked %d times", api.calls)
			}
		})
	}
}

func TestSubjectConstructionPreservesAuthenticationErrorThroughBindingContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/subject", nil)
	ginContext, _ := httprouter.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request
	_, err := constructOperationRequest(OperationDescription{
		InputType: reflect.TypeFor[subjectRequest](),
		RequestBindings: []RequestBinding{{
			Source:     BindingSourceSubject,
			FieldPath:  []string{"Subject"},
			FieldIndex: []int{0},
			Input:      InputOf[string](Required(), MinLength(1)),
		}},
	}, ginContext)
	if !errors.Is(err, internalauthorization.ErrAuthenticationRequired) {
		t.Fatalf("constructOperationRequest() error = %v, want wrapped authentication-required error", err)
	}
}

type optionalBodyPayload struct {
	Name string `json:"name"`
}

type optionalBodyRequest struct {
	Payload *optionalBodyPayload
}

type optionalBodyPayloadContract struct {
	Name Binding[string]
}

type optionalBodyRequestContract struct {
	Payload BodyObjectBinding[optionalBodyPayloadContract]
}

type optionalBodyAPI struct {
	received *optionalBodyRequest
}

func (*optionalBodyAPI) Routes() map[string]string {
	return map[string]string{"Execute": "/optional-body"}
}

func (*optionalBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Execute": {Request: optionalBodyRequestContract{
			Payload: BodyObject(optionalBodyPayloadContract{
				Name: Value[string](Required()),
			}),
		}},
	}}
}

func (api *optionalBodyAPI) Execute(_ context.Context, request optionalBodyRequest) map[string]bool {
	api.received = &request
	return map[string]bool{"body": request.Payload != nil}
}

func TestOptionalBodyDoesNotAllocateOrValidateDescendantsWhenAbsent(t *testing.T) {
	api := &optionalBodyAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	request := httptest.NewRequest(http.MethodPost, "/optional-body", nil)
	response := httptest.NewRecorder()
	app.(WithRouter).Router().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if api.received == nil || api.received.Payload != nil {
		t.Fatalf("optional absent body = %#v, want nil payload", api.received)
	}
}

type scalarBodyAPI struct {
	received string
}

func (*scalarBodyAPI) Routes() map[string]string {
	return map[string]string{"Echo": "/scalar-body"}
}

func (*scalarBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Echo": {Request: Body[string](Default("hello"), MinLength(3), MaxLength(10))},
	}}
}

func (api *scalarBodyAPI) Echo(_ context.Context, value string) string {
	api.received = value
	return value
}

func TestRootBodyUsesDefaultAndValidatesPresentValue(t *testing.T) {
	api := &scalarBodyAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/scalar-body", nil))
	if missing.Code != http.StatusOK || api.received != "hello" {
		t.Fatalf("missing body = %d/%q, want default hello: %s", missing.Code, api.received, missing.Body.String())
	}

	invalid := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/scalar-body", strings.NewReader(`"no"`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "length must be at least 3") {
		t.Fatalf("invalid body = %d %s", invalid.Code, invalid.Body.String())
	}

	nullBody := httptest.NewRecorder()
	nullRequest := httptest.NewRequest(http.MethodPost, "/scalar-body", strings.NewReader(`null`))
	nullRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(nullBody, nullRequest)
	if nullBody.Code != http.StatusBadRequest || !strings.Contains(nullBody.Body.String(), "must not be null") {
		t.Fatalf("null body = %d %s", nullBody.Code, nullBody.Body.String())
	}
}

type collectionPresenceChild struct {
	Rank int `json:"rank"`
}

type collectionPresenceDetails struct {
	Enabled bool `json:"enabled"`
}

type collectionPresenceElement struct {
	Count    int                       `json:"count"`
	Label    string                    `json:"label"`
	Details  collectionPresenceDetails `json:"details"`
	Children []collectionPresenceChild `json:"children"`
}

type collectionPresenceChildContract struct {
	Rank Binding[int]
}

type collectionPresenceDetailsContract struct {
	Enabled Binding[bool]
}

type collectionPresenceElementContract struct {
	Count    Binding[int]
	Label    Binding[string]
	Details  collectionPresenceDetailsContract
	Children Binding[[]collectionPresenceChild]
}

type collectionPresenceAPI struct {
	calls int
}

func (*collectionPresenceAPI) Routes() map[string]string {
	return map[string]string{"Create": "/collection-presence"}
}

func (*collectionPresenceAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Create": {Request: Body[[]collectionPresenceElement](
			Required(),
			Elements(ElementObject[collectionPresenceElement](collectionPresenceElementContract{
				Count:   Value[int](Required()),
				Label:   Value[string](Required()),
				Details: collectionPresenceDetailsContract{Enabled: Value[bool](Required())},
				Children: Value[[]collectionPresenceChild](Elements(
					ElementObject[collectionPresenceChild](collectionPresenceChildContract{
						Rank: Value[int](Required()),
					}),
				)),
			})),
		)},
	}}
}

func (api *collectionPresenceAPI) Create(context.Context, []collectionPresenceElement) {
	api.calls++
}

func TestStructuredCollectionRequiredFieldsUseRawJSONPresence(t *testing.T) {
	api := &collectionPresenceAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "omitted required integer",
			body:       `[{"label":"","details":{"enabled":false},"children":[{"rank":0}]}]`,
			wantStatus: http.StatusBadRequest,
			wantError:  `field \"Count\": is required`,
		},
		{
			name:       "omitted required string",
			body:       `[{"count":0,"details":{"enabled":false},"children":[{"rank":0}]}]`,
			wantStatus: http.StatusBadRequest,
			wantError:  `field \"Label\": is required`,
		},
		{
			name:       "omitted required boolean",
			body:       `[{"count":0,"label":"","details":{},"children":[{"rank":0}]}]`,
			wantStatus: http.StatusBadRequest,
			wantError:  `field \"Details.Enabled\": is required`,
		},
		{
			name:       "omitted nested collection field",
			body:       `[{"count":0,"label":"","details":{"enabled":false},"children":[{}]}]`,
			wantStatus: http.StatusBadRequest,
			wantError:  `field \"Rank\": is required`,
		},
		{
			name:       "explicit zero values",
			body:       `[{"count":0,"label":"","details":{"enabled":false},"children":[{"rank":0}]}]`,
			wantStatus: http.StatusNoContent,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			callsBefore := api.calls
			request := httptest.NewRequest(http.MethodPost, "/collection-presence", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantError != "" {
				if !strings.Contains(response.Body.String(), test.wantError) {
					t.Fatalf("response = %s, want %q", response.Body.String(), test.wantError)
				}
				if api.calls != callsBefore {
					t.Fatal("API function was invoked for an invalid collection")
				}
			} else if api.calls != callsBefore+1 {
				t.Fatal("API function was not invoked for a valid collection")
			}
		})
	}
}

type collectionDefaultChild struct {
	Rank int `json:"rank"`
}

type collectionDefaultElement struct {
	Name     string                   `json:"name"`
	Children []collectionDefaultChild `json:"children"`
}

type collectionDefaultChildContract struct {
	Rank Binding[int]
}

type collectionDefaultElementContract struct {
	Name     Binding[string]
	Children Binding[[]collectionDefaultChild]
}

type collectionDefaultAPI struct {
	received []collectionDefaultElement
}

func (*collectionDefaultAPI) Routes() map[string]string {
	return map[string]string{"Create": "/collection-defaults"}
}

func (*collectionDefaultAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Create": {Request: Body[[]collectionDefaultElement](
			Required(),
			Elements(ElementObject[collectionDefaultElement](collectionDefaultElementContract{
				Name: Value[string](Default("fallback")),
				Children: Value[[]collectionDefaultChild](
					Default([]collectionDefaultChild{}),
					Elements(ElementObject[collectionDefaultChild](collectionDefaultChildContract{
						Rank: Value[int](Default(7), Min(1)),
					})),
				),
			})),
		)},
	}}
}

func (api *collectionDefaultAPI) Create(_ context.Context, request []collectionDefaultElement) {
	api.received = request
}

func TestStructuredCollectionDefaultsAreAppliedBeforeInvocation(t *testing.T) {
	api := &collectionDefaultAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	request := httptest.NewRequest(
		http.MethodPost,
		"/collection-defaults",
		strings.NewReader(`[{}, {"name":"explicit","children":[{}]}]`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if len(api.received) != 2 || api.received[0].Name != "fallback" ||
		api.received[0].Children == nil || len(api.received[0].Children) != 0 {
		t.Fatalf("first element defaults = %#v", api.received)
	}
	if api.received[1].Name != "explicit" || len(api.received[1].Children) != 1 ||
		api.received[1].Children[0].Rank != 7 {
		t.Fatalf("nested element defaults = %#v", api.received)
	}
}

type compositeDefaultRequest struct {
	Attributes map[string]any `json:"attributes,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
}

type compositeDefaultContract struct {
	Attributes Binding[map[string]any]
	Tags       Binding[[]string]
}

type compositeDefaultAPI struct {
	receivedStarting []string
	receivedTags     []bool
}

func (*compositeDefaultAPI) Routes() map[string]string {
	return map[string]string{"Create": "/composite-defaults"}
}

func (*compositeDefaultAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Create": {Request: BodyObject(compositeDefaultContract{
			Attributes: Value[map[string]any](Default(map[string]any{
				"nested": map[string]any{"values": []any{"original"}},
			})),
			Tags: Value[[]string](Default([]string{})),
		}, Required())},
	}}
}

func (api *compositeDefaultAPI) Create(_ context.Context, request compositeDefaultRequest) {
	api.receivedStarting = append(
		api.receivedStarting,
		request.Attributes["nested"].(map[string]any)["values"].([]any)[0].(string),
	)
	api.receivedTags = append(api.receivedTags, request.Tags != nil)
	request.Attributes["nested"].(map[string]any)["values"].([]any)[0] = "mutated"
	request.Tags = append(request.Tags, "mutated")
}

func TestBodyValueCompositeDefaultsAreAppliedIndependently(t *testing.T) {
	api := &compositeDefaultAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/composite-defaults", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("attempt %d status = %d: %s", attempt+1, response.Code, response.Body.String())
		}
	}
	if got := api.receivedStarting; !reflect.DeepEqual(got, []string{"original", "original"}) {
		t.Fatalf("defaults at invocation start = %#v, want independent original values", got)
	}
	if got := api.receivedTags; !reflect.DeepEqual(got, []bool{true, true}) {
		t.Fatalf("empty slice defaults were nil: %#v", got)
	}
}

type invalidDefaultBodyPayload struct {
	Name string `json:"name"`
}

type invalidDefaultBodyContract struct {
	Name Binding[string]
}

type invalidDefaultBodyAPI struct{}

func (*invalidDefaultBodyAPI) Create(context.Context, invalidDefaultBodyPayload) {}

func (*invalidDefaultBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Create": {Request: BodyObject(invalidDefaultBodyContract{
			Name: Value[string](MinLength(3)),
		}, Default(invalidDefaultBodyPayload{Name: "x"}))},
	}}
}

func TestBodyObjectDefaultHonorsDescendantContracts(t *testing.T) {
	_, err := DescribeReflectedAPI((*invalidDefaultBodyAPI)(nil))
	if err == nil || !strings.Contains(err.Error(), "length must be at least 3") {
		t.Fatalf("DescribeReflectedAPI() error = %v, want invalid default rejection", err)
	}
}

type nonNullableCollectionAPI struct{}

func (*nonNullableCollectionAPI) Create(context.Context, []string) {}

func (*nonNullableCollectionAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Create": {Request: Body[[]string](Elements(Element[string]()))},
	}}
}

func TestTransportRejectsNullNonNullableCollectionElement(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&nonNullableCollectionAPI{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/Create", strings.NewReader(`[null]`))
	request.Header.Set("Content-Type", "application/json")
	app.(WithRouter).Router().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "item 1: must not be null") {
		t.Fatalf("null element response = %d: %s", response.Code, response.Body.String())
	}
}

type optionalPointerBodyAPI struct {
	received *optionalBodyPayload
}

func (*optionalPointerBodyAPI) Routes() map[string]string {
	return map[string]string{"Execute": "/optional-pointer-body"}
}

func (*optionalPointerBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Execute": {Request: Body[*optionalBodyPayload](Nullable())},
	}}
}

func (api *optionalPointerBodyAPI) Execute(_ context.Context, request *optionalBodyPayload) map[string]bool {
	api.received = request
	return map[string]bool{"body": request != nil}
}

func TestOptionalRootPointerBodyPreservesAbsence(t *testing.T) {
	api := &optionalPointerBodyAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/optional-pointer-body", nil))
	if missing.Code != http.StatusOK || api.received != nil {
		t.Fatalf("missing pointer body = %d/%#v: %s", missing.Code, api.received, missing.Body.String())
	}

	present := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/optional-pointer-body", strings.NewReader(`{"name":"present"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(present, request)
	if present.Code != http.StatusOK || api.received == nil || api.received.Name != "present" {
		t.Fatalf("present pointer body = %d/%#v: %s", present.Code, api.received, present.Body.String())
	}

	explicitNull := httptest.NewRecorder()
	nullRequest := httptest.NewRequest(http.MethodPost, "/optional-pointer-body", strings.NewReader(`null`))
	nullRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(explicitNull, nullRequest)
	if explicitNull.Code != http.StatusOK || api.received != nil {
		t.Fatalf("nullable body = %d/%#v: %s", explicitNull.Code, api.received, explicitNull.Body.String())
	}
}

type nullableBodyObjectPayload struct {
	Name string `json:"name"`
}

type nullableBodyObjectContract struct {
	Name Binding[string]
}

type nullableBodyObjectAPI struct {
	received *nullableBodyObjectPayload
}

func (*nullableBodyObjectAPI) Routes() map[string]string {
	return map[string]string{"Execute": "/nullable-body-object"}
}

func (*nullableBodyObjectAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Execute": {
			Request: BodyObject(nullableBodyObjectContract{
				Name: Value[string](Required()),
			}, Nullable()),
		},
	}}
}

func (api *nullableBodyObjectAPI) Execute(
	_ context.Context,
	request *nullableBodyObjectPayload,
) map[string]bool {
	api.received = request
	return map[string]bool{"body": request != nil}
}

func TestNullableBodyObjectSkipsDescendantPresenceRulesForNullRoot(t *testing.T) {
	api := &nullableBodyObjectAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	explicitNull := httptest.NewRecorder()
	nullRequest := httptest.NewRequest(
		http.MethodPost,
		"/nullable-body-object",
		strings.NewReader(`null`),
	)
	nullRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(explicitNull, nullRequest)
	if explicitNull.Code != http.StatusOK || api.received != nil {
		t.Fatalf(
			"nullable body object = %d/%#v: %s",
			explicitNull.Code,
			api.received,
			explicitNull.Body.String(),
		)
	}

	emptyObject := httptest.NewRecorder()
	emptyRequest := httptest.NewRequest(
		http.MethodPost,
		"/nullable-body-object",
		strings.NewReader(`{}`),
	)
	emptyRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(emptyObject, emptyRequest)
	if emptyObject.Code != http.StatusBadRequest || !strings.Contains(emptyObject.Body.String(), "is required") {
		t.Fatalf("empty body object = %d %s", emptyObject.Code, emptyObject.Body.String())
	}
}

type objectRulePayload struct {
	Name      *string  `json:"name,omitempty"`
	Latitude  *float64 `json:"lat,omitempty"`
	Longitude *float64 `json:"lon,omitempty"`
	Details   *struct {
		Note *string `json:"note,omitempty"`
	} `json:"details,omitempty"`
}

type objectRulePayloadContract struct {
	Name      Binding[*string]
	Latitude  Binding[*float64]
	Longitude Binding[*float64]
}

type objectRuleRequest struct {
	Payload objectRulePayload
}

type objectRuleRequestContract struct {
	Payload BodyObjectBinding[objectRulePayloadContract]
}

type objectRuleAPI struct {
	calls    int
	received objectRulePayload
}

func (*objectRuleAPI) Routes() map[string]string {
	return map[string]string{"Update": "/object-rules"}
}

func (*objectRuleAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Update": {Request: objectRuleRequestContract{
			Payload: BodyObject(objectRulePayloadContract{
				Name:      Value[*string](),
				Latitude:  Value[*float64](Nullable(), Min(-90.0), Max(90.0)),
				Longitude: Value[*float64](Nullable(), Min(-180.0), Max(180.0)),
			}, Required()).WithRules(
				AtLeastOne("Name", "Latitude", "Longitude", "Details.Note"),
				AllOrNone("Latitude", "Longitude"),
				SameNullState("Latitude", "Longitude"),
			),
		}},
	}}
}

func (api *objectRuleAPI) Update(_ context.Context, request objectRuleRequest) map[string]bool {
	api.calls++
	api.received = request.Payload
	return map[string]bool{"ok": true}
}

func TestBodyObjectRulesValidateJSONPresenceBeforeInvocation(t *testing.T) {
	api := &objectRuleAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{name: "empty patch", body: `{}`, wantStatus: http.StatusBadRequest, wantError: "at least one of name, lat, lon, details.note must be provided"},
		{name: "partial pair", body: `{"lat":19.4}`, wantStatus: http.StatusBadRequest, wantError: "lat, lon must be provided together"},
		{name: "ordinary field", body: `{"name":"updated"}`, wantStatus: http.StatusOK},
		{name: "complete pair", body: `{"lat":19.4,"lon":-99.1}`, wantStatus: http.StatusOK},
		{name: "explicit null pair", body: `{"lat":null,"lon":null}`, wantStatus: http.StatusOK},
		{name: "null latitude with longitude", body: `{"lat":null,"lon":-99.1}`, wantStatus: http.StatusBadRequest, wantError: "lat, lon must be either all null or all non-null"},
		{name: "latitude with null longitude", body: `{"lat":19.4,"lon":null}`, wantStatus: http.StatusBadRequest, wantError: "lat, lon must be either all null or all non-null"},
		{name: "noncanonical JSON name", body: `{"LAT":19.4,"lon":-99.1}`, wantStatus: http.StatusBadRequest, wantError: `property \"LAT\" must use canonical JSON name \"lat\"`},
		{name: "canonical and case alias", body: `{"lat":19.4,"LAT":20,"lon":-99.1}`, wantStatus: http.StatusBadRequest, wantError: `property \"LAT\" must use canonical JSON name \"lat\"`},
		{name: "nested field", body: `{"details":{"note":"updated"}}`, wantStatus: http.StatusOK},
		{name: "explicit null nested leaf", body: `{"details":{"note":null}}`, wantStatus: http.StatusOK},
		{name: "null nested ancestor", body: `{"details":null}`, wantStatus: http.StatusBadRequest, wantError: "at least one of name, lat, lon, details.note must be provided"},
	} {
		t.Run(test.name, func(t *testing.T) {
			callsBefore := api.calls
			request := httptest.NewRequest(http.MethodPost, "/object-rules", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantError != "" {
				if !strings.Contains(response.Body.String(), test.wantError) {
					t.Fatalf("response = %s, want error containing %q", response.Body.String(), test.wantError)
				}
				if api.calls != callsBefore {
					t.Fatal("API function was invoked for an invalid object")
				}
			} else if api.calls != callsBefore+1 {
				t.Fatal("API function was not invoked for a valid object")
			}
		})
	}
}

type collectionBodyAPI struct {
	calls int
}

func (*collectionBodyAPI) Routes() map[string]string {
	return map[string]string{
		"Replace":         "/labels",
		"ReplaceOptional": "/optional-labels",
	}
}

func (*collectionBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Replace":         {Request: Body[[]string](Required(), MinItems(1), MaxItems(2))},
		"ReplaceOptional": {Request: Body[[]string](Nullable(), MinItems(1), MaxItems(2))},
	}}
}

func (api *collectionBodyAPI) Replace(_ context.Context, _ []string) map[string]bool {
	api.calls++
	return map[string]bool{"ok": true}
}

func (api *collectionBodyAPI) ReplaceOptional(_ context.Context, _ []string) map[string]bool {
	api.calls++
	return map[string]bool{"ok": true}
}

func TestCollectionCardinalityValidatesRootBodiesBeforeInvocation(t *testing.T) {
	api := &collectionBodyAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "empty", body: `[]`, wantStatus: http.StatusBadRequest},
		{name: "minimum", body: `["one"]`, wantStatus: http.StatusOK},
		{name: "maximum", body: `["one","two"]`, wantStatus: http.StatusOK},
		{name: "too many", body: `["one","two","three"]`, wantStatus: http.StatusBadRequest},
		{name: "null", body: `null`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			callsBefore := api.calls
			request := httptest.NewRequest(http.MethodPost, "/labels", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			wantCalls := callsBefore
			if test.wantStatus == http.StatusOK {
				wantCalls++
			}
			if api.calls != wantCalls {
				t.Fatalf("calls = %d, want %d", api.calls, wantCalls)
			}
		})
	}
}

func TestNullableCollectionCardinalityDistinguishesNullFromEmptyArray(t *testing.T) {
	api := &collectionBodyAPI{}
	app := MakeRawApp()
	app.AddApi(api)
	router := app.(WithRouter).Router()

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "null", body: `null`, wantStatus: http.StatusOK},
		{name: "empty array", body: `[]`, wantStatus: http.StatusBadRequest},
		{name: "one item", body: `["one"]`, wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			callsBefore := api.calls
			request := httptest.NewRequest(http.MethodPost, "/optional-labels", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			wantCalls := callsBefore
			if test.wantStatus == http.StatusOK {
				wantCalls++
			}
			if api.calls != wantCalls {
				t.Fatalf("calls = %d, want %d", api.calls, wantCalls)
			}
		})
	}
}

func jsonBody(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(encoded)
}
