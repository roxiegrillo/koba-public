package koba

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

type optionalPointerBodyAPI struct {
	received *optionalBodyPayload
}

func (*optionalPointerBodyAPI) Routes() map[string]string {
	return map[string]string{"Execute": "/optional-pointer-body"}
}

func (*optionalPointerBodyAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Execute": {Request: Body[*optionalBodyPayload]()},
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
}

func jsonBody(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(encoded)
}
