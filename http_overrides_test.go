package koba

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type (
	overrideFixtureAPI     struct{}
	overrideFixtureRequest struct {
		Name string `json:"name"`
	}
)

func (*overrideFixtureAPI) Create(overrideFixtureRequest) map[string]bool {
	return map[string]bool{"created": true}
}

func (*overrideFixtureAPI) Update(overrideFixtureRequest) map[string]bool {
	return map[string]bool{"updated": true}
}

func (*overrideFixtureAPI) Routes() map[string]string {
	return map[string]string{"Create": "/overrides", "Update": "/overrides/:id"}
}

func (*overrideFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{"Create": {SuccessStatus: http.StatusCreated}, "Update": {Method: http.MethodPatch}}}
}

func TestHTTPMethodAndSuccessStatusOverrides(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&overrideFixtureAPI{})
	router := app.(WithRouter).Router()
	created := httptest.NewRecorder()
	router.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/overrides", strings.NewReader(`{"name":"new"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d want=%d", created.Code, http.StatusCreated)
	}
	updated := httptest.NewRecorder()
	router.ServeHTTP(updated, httptest.NewRequest(http.MethodPatch, "/overrides/item-1", strings.NewReader(`{"name":"changed"}`)))
	if updated.Code != http.StatusOK {
		t.Fatalf("patch status=%d want=%d", updated.Code, http.StatusOK)
	}
}
