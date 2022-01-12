package koba

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type greetings struct {
	calls int
}

func (api *greetings) Hello() map[string]string {
	api.calls++
	return map[string]string{"hello": "world"}
}

func (*greetings) Nothing() {}

func (*greetings) Fail() {
	panic(errors.New("the greeting failed"))
}

func (*greetings) FailWithValue() {
	panic(42)
}

func (*greetings) ListHTTPStatuses() []int {
	return []int{http.StatusOK}
}

type renamed struct{}

func (*renamed) Hello() string { return "hi" }

func (*renamed) Other() string { return "other" }

func (*renamed) Routes() Routes {
	return Routes{"Hello": "/greeting"}
}

type polls struct {
	accepted []ballot
}

type ballot struct {
	Nullifier string `json:"nullifier"`
	Sealed    string `json:"sealed"`
}

func (api *polls) Cast(request ballot) ballot {
	api.accepted = append(api.accepted, request)
	return request
}

func (api *polls) Count() int {
	return len(api.accepted)
}

func (*polls) Forget(ballot) {}

type takesTwo struct{}

func (*takesTwo) Do(int, int) {}

type takesPointer struct{}

func (*takesPointer) Do(*ballot) {}

type returnsTwo struct{}

func (*returnsTwo) Do() (int, int) { return 1, 2 }

func call(t *testing.T, served App, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, served, method, target, "")
}

func send(t *testing.T, served App, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	served.(WithRouter).Router().ServeHTTP(response, httptest.NewRequest(method, target, strings.NewReader(body)))
	return response
}

func TestTheHTTPMethodFollowsTheSignature(t *testing.T) {
	api := &polls{}
	served := MakeRawApp()
	served.AddApi(api)

	response := send(t, served, http.MethodPost, "/cast", `{"nullifier":"n1","sealed":"s1"}`)
	if response.Code != http.StatusOK || response.Body.String() != `{"nullifier":"n1","sealed":"s1"}` {
		t.Fatalf("POST /cast = %d %q", response.Code, response.Body.String())
	}
	if len(api.accepted) != 1 || api.accepted[0].Nullifier != "n1" {
		t.Fatalf("accepted = %+v", api.accepted)
	}
	if response := call(t, served, http.MethodGet, "/count"); response.Code != http.StatusOK || response.Body.String() != "1" {
		t.Fatalf("GET /count = %d %q", response.Code, response.Body.String())
	}
	if response := send(t, served, http.MethodPost, "/forget", `{}`); response.Code != http.StatusNoContent {
		t.Fatalf("POST /forget = %d", response.Code)
	}
	if response := call(t, served, http.MethodGet, "/cast"); response.Code != http.StatusNotFound {
		t.Fatalf("GET /cast = %d, want 404: a method with a request is not served over GET", response.Code)
	}
	if response := send(t, served, http.MethodPost, "/count", "{}"); response.Code != http.StatusNotFound {
		t.Fatalf("POST /count = %d, want 404: a method without a request is not served over POST", response.Code)
	}
}

func TestABodyThatDoesNotDecodeIsRejected(t *testing.T) {
	served := MakeRawApp()
	served.AddApi(&polls{})
	for name, body := range map[string]string{"malformed": `{"nullifier":`, "empty": "", "wrong type": `{"nullifier":2}`} {
		t.Run(name, func(t *testing.T) {
			response := send(t, served, http.MethodPost, "/cast", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			var decoded map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["error"] != "bad_request" || !strings.HasPrefix(decoded["message"].(string), "request body: ") {
				t.Fatalf("body = %v", decoded)
			}
		})
	}
}

func TestExportedMethodsAreEndpoints(t *testing.T) {
	api := &greetings{}
	served := MakeRawApp()
	served.AddApi(api)

	response := call(t, served, http.MethodGet, "/hello")
	if response.Code != http.StatusOK || response.Body.String() != `{"hello":"world"}` {
		t.Fatalf("GET /hello = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q", got)
	}
	if api.calls != 1 {
		t.Fatalf("calls = %d, want 1", api.calls)
	}
	if response := call(t, served, http.MethodGet, "/nothing"); response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("GET /nothing = %d %q", response.Code, response.Body.String())
	}
	if response := call(t, served, http.MethodGet, "/list-http-statuses"); response.Code != http.StatusOK || response.Body.String() != "[200]" {
		t.Fatalf("GET /list-http-statuses = %d %q", response.Code, response.Body.String())
	}
	if response := call(t, served, http.MethodGet, "/missing"); response.Code != http.StatusNotFound {
		t.Fatalf("GET /missing = %d", response.Code)
	}
}

func TestAFailingMethodIsAnsweredWithTheFailure(t *testing.T) {
	served := MakeRawApp()
	served.AddApi(&greetings{})
	for target, message := range map[string]string{"/fail": "the greeting failed", "/fail-with-value": "42"} {
		response := call(t, served, http.MethodGet, target)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("GET %s = %d", target, response.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != float64(500) || body["error"] != "internal_server_error" || body["message"] != message {
			t.Fatalf("GET %s body = %v", target, body)
		}
	}
}

func TestAnAPIChoosesItsRoutes(t *testing.T) {
	served := MakeRawApp()
	served.AddApi(&renamed{})
	if response := call(t, served, http.MethodGet, "/greeting"); response.Code != http.StatusOK || response.Body.String() != `"hi"` {
		t.Fatalf("GET /greeting = %d %q", response.Code, response.Body.String())
	}
	if response := call(t, served, http.MethodGet, "/other"); response.Code != http.StatusOK {
		t.Fatalf("GET /other = %d", response.Code)
	}
	for _, target := range []string{"/hello", "/routes"} {
		if response := call(t, served, http.MethodGet, target); response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", target, response.Code)
		}
	}
}

func TestRegistrationRefusesShapesNoEndpointCanServe(t *testing.T) {
	for name, api := range map[string]any{"two parameters": &takesTwo{}, "pointer parameter": &takesPointer{}, "two results": &returnsTwo{}, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("AddApi did not panic")
				}
			}()
			MakeRawApp().AddApi(api)
		})
	}
}

func TestRouteOf(t *testing.T) {
	for name, want := range map[string]string{
		"Hello":            "/hello",
		"DeclarePoll":      "/declare-poll",
		"ListHTTPStatuses": "/list-http-statuses",
		"HTTP":             "/http",
		"Item2Get":         "/item2-get",
		"A":                "/a",
	} {
		if got := RouteOf(name); got != want {
			t.Errorf("RouteOf(%q) = %q, want %q", name, got, want)
		}
	}
}
