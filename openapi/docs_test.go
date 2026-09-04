package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	fb "github.com/roxiegrillo/koba/v2"
)

type RuntimeDocsResponse struct {
	Status string `json:"status"`
}

type runtimeDocsAPI struct{}

func (*runtimeDocsAPI) Health() RuntimeDocsResponse {
	return RuntimeDocsResponse{Status: "ok"}
}

func (*runtimeDocsAPI) Routes() map[string]string {
	return map[string]string{"Health": "/health"}
}

func TestMountServesRuntimeSpecAndDocumentation(t *testing.T) {
	app := fb.MakeRawApp()
	app.AddApi(&runtimeDocsAPI{})

	document, err := Mount(app, Config{Title: "Runtime API", Version: "1.0.0"}, DocsOptions{
		DocsPath: "/reference",
		SpecPath: "/contracts/openapi.json",
	})
	if err != nil {
		t.Fatalf("Mount() returned an error: %v", err)
	}
	if document.Paths.Find("/health") == nil {
		t.Fatal("mounted document does not contain the registered API")
	}

	router := app.(fb.WithRouter).Router()

	specResponse := httptest.NewRecorder()
	router.ServeHTTP(specResponse, httptest.NewRequest(http.MethodGet, "/contracts/openapi.json", nil))
	if got, want := specResponse.Code, http.StatusOK; got != want {
		t.Fatalf("spec status = %d, want %d: %s", got, want, specResponse.Body.String())
	}
	if got, want := specResponse.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("spec Cache-Control = %q, want %q", got, want)
	}
	var rawDocument map[string]any
	if err := json.Unmarshal(specResponse.Body.Bytes(), &rawDocument); err != nil {
		t.Fatalf("spec response is not JSON: %v", err)
	}
	if got, want := rawDocument["openapi"], Version; got != want {
		t.Errorf("runtime OpenAPI version = %#v, want %#v", got, want)
	}

	redirectResponse := httptest.NewRecorder()
	router.ServeHTTP(redirectResponse, httptest.NewRequest(http.MethodGet, "/reference", nil))
	if got, want := redirectResponse.Code, http.StatusPermanentRedirect; got != want {
		t.Fatalf("docs redirect status = %d, want %d", got, want)
	}
	if got, want := redirectResponse.Header().Get("Location"), "/reference/"; got != want {
		t.Errorf("docs redirect location = %q, want %q", got, want)
	}

	docsResponse := httptest.NewRecorder()
	router.ServeHTTP(docsResponse, httptest.NewRequest(http.MethodGet, "/reference/", nil))
	if got, want := docsResponse.Code, http.StatusOK; got != want {
		t.Fatalf("docs status = %d, want %d: %s", got, want, docsResponse.Body.String())
	}
	body := docsResponse.Body.String()
	if !strings.Contains(body, "Runtime API documentation") {
		t.Errorf("docs page does not contain generated title")
	}
	if !strings.Contains(body, "../contracts/openapi.json") {
		t.Errorf("docs page does not reference the runtime spec: %s", body)
	}

	assetResponse := httptest.NewRecorder()
	router.ServeHTTP(assetResponse, httptest.NewRequest(http.MethodGet, "/reference/swagger-ui.css", nil))
	if got, want := assetResponse.Code, http.StatusOK; got != want {
		t.Fatalf("embedded Swagger UI asset status = %d, want %d", got, want)
	}
	if assetResponse.Body.Len() == 0 {
		t.Fatal("embedded Swagger UI asset is empty")
	}
}

func TestRegisterDocsSupportsRouterGroups(t *testing.T) {
	router := httprouter.New()
	group := router.Group("/service")
	document, err := Generate(Config{Title: "Runtime API", Version: "1"}, (*runtimeDocsAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	if err := RegisterDocs(group, document, DocsOptions{
		DocsPath: "/reference",
		SpecPath: "/contracts/openapi.json",
	}); err != nil {
		t.Fatalf("RegisterDocs() returned an error: %v", err)
	}

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/service/reference", nil))
	if got, want := redirect.Header().Get("Location"), "/service/reference/"; got != want {
		t.Fatalf("group docs redirect = %q, want %q", got, want)
	}

	docs := httptest.NewRecorder()
	router.ServeHTTP(docs, httptest.NewRequest(http.MethodGet, "/service/reference/", nil))
	if got, want := docs.Code, http.StatusOK; got != want {
		t.Fatalf("group docs status = %d, want %d: %s", got, want, docs.Body.String())
	}
	if !strings.Contains(docs.Body.String(), "/service/reference/swagger-ui.css") {
		t.Fatalf("group docs do not use the public asset base: %s", docs.Body.String())
	}
}

func TestRegisterJSONDocsServesValidatedStaticDocument(t *testing.T) {
	router := httprouter.New()
	contents := []byte(`{
  "openapi": "3.0.3",
  "info": {
    "title": "Committed API",
    "version": "2.1.0"
  },
  "paths": {
    "/health": {
      "get": {
        "operationId": "health",
        "responses": {
          "200": {
            "description": "Healthy"
          }
        }
      }
    }
  }
}`)

	document, err := RegisterJSONDocs(router, contents, DocsOptions{
		DocsPath: "/reference",
		SpecPath: "/contracts/openapi.json",
	})
	if err != nil {
		t.Fatalf("RegisterJSONDocs() returned an error: %v", err)
	}
	if document.Paths.Find("/health") == nil {
		t.Fatal("parsed document does not contain the committed API path")
	}

	specResponse := httptest.NewRecorder()
	router.ServeHTTP(specResponse, httptest.NewRequest(http.MethodGet, "/contracts/openapi.json", nil))
	if got, want := specResponse.Code, http.StatusOK; got != want {
		t.Fatalf("static spec status = %d, want %d: %s", got, want, specResponse.Body.String())
	}
	if got, want := specResponse.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("static spec Cache-Control = %q, want %q", got, want)
	}
	var servedDocument map[string]any
	if err := json.Unmarshal(specResponse.Body.Bytes(), &servedDocument); err != nil {
		t.Fatalf("static spec response is not JSON: %v", err)
	}
	info, ok := servedDocument["info"].(map[string]any)
	if !ok {
		t.Fatalf("static spec info = %#v, want an object", servedDocument["info"])
	}
	if got, want := info["version"], "2.1.0"; got != want {
		t.Errorf("static spec version = %#v, want %#v", got, want)
	}

	redirectResponse := httptest.NewRecorder()
	router.ServeHTTP(redirectResponse, httptest.NewRequest(http.MethodGet, "/reference", nil))
	if got, want := redirectResponse.Code, http.StatusPermanentRedirect; got != want {
		t.Fatalf("static docs redirect status = %d, want %d", got, want)
	}

	docsResponse := httptest.NewRecorder()
	router.ServeHTTP(docsResponse, httptest.NewRequest(http.MethodGet, "/reference/", nil))
	if got, want := docsResponse.Code, http.StatusOK; got != want {
		t.Fatalf("static docs status = %d, want %d: %s", got, want, docsResponse.Body.String())
	}
	if !strings.Contains(docsResponse.Body.String(), "Committed API documentation") {
		t.Errorf("static docs page does not use the committed document title")
	}
	if !strings.Contains(docsResponse.Body.String(), "../contracts/openapi.json") {
		t.Errorf("static docs page does not reference the configured static spec")
	}
}

func TestMountJSONUsesStandardServiceEndpoints(t *testing.T) {
	app := fb.MakeRawApp()
	app.AddApi(&runtimeDocsAPI{})
	contents, err := GenerateJSON(Config{Title: "Committed API", Version: "1.0.0"}, false, (*runtimeDocsAPI)(nil))
	if err != nil {
		t.Fatalf("GenerateJSON() returned an error: %v", err)
	}

	if _, err := MountJSON(app, contents, DocsOptions{}); err != nil {
		t.Fatalf("MountJSON() returned an error: %v", err)
	}
	router := app.(fb.WithRouter).Router()

	for _, endpoint := range []string{DefaultSpecPath, DefaultDocsPath + "/"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if got, want := response.Code, http.StatusOK; got != want {
			t.Errorf("GET %s status = %d, want %d: %s", endpoint, got, want, response.Body.String())
		}
	}

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, DefaultDocsPath, nil))
	if got, want := redirect.Code, http.StatusPermanentRedirect; got != want {
		t.Fatalf("GET %s status = %d, want %d", DefaultDocsPath, got, want)
	}
	if got, want := redirect.Header().Get("Location"), DefaultDocsPath+"/"; got != want {
		t.Fatalf("documentation redirect = %q, want %q", got, want)
	}
}

func TestMountJSONRequiresServiceTransport(t *testing.T) {
	if _, err := MountJSON(struct{}{}, []byte(`{}`), DocsOptions{}); err == nil || !strings.Contains(err.Error(), "Gin transport") {
		t.Fatalf("MountJSON() error = %v, want missing transport error", err)
	}
}

func TestRegisterJSONDocsRejectsInvalidJSON(t *testing.T) {

	_, err := RegisterJSONDocs(httprouter.New(), []byte(`{"openapi":`), DocsOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("RegisterJSONDocs() error = %v, want invalid-JSON error", err)
	}
}

func TestRegisterJSONDocsRejectsInvalidOpenAPIDocument(t *testing.T) {
	contents := []byte(`{
  "openapi": "3.0.3",
  "info": {
    "title": "Missing version"
  },
  "paths": {}
}`)

	_, err := RegisterJSONDocs(httprouter.New(), contents, DocsOptions{})
	if err == nil || !strings.Contains(err.Error(), "validate static document") {
		t.Fatalf("RegisterJSONDocs() error = %v, want document-validation error", err)
	}
}

func TestRegisterDocsRejectsSpecNestedUnderDocs(t *testing.T) {
	document, err := Generate(Config{Title: "Runtime API", Version: "1"}, (*runtimeDocsAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	err = RegisterDocs(httprouter.New(), document, DocsOptions{
		DocsPath: "/reference",
		SpecPath: "/reference/openapi.json",
	})
	if err == nil || !strings.Contains(err.Error(), "must not be nested") {
		t.Fatalf("RegisterDocs() error = %v, want nested-path error", err)
	}
}
