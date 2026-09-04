package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	fb "github.com/roxiegrillo/koba/v2"
	"github.com/swaggest/swgui/v5emb"
)

// DocsOptions control the runtime OpenAPI and Swagger UI endpoints.
type DocsOptions struct {
	DocsPath string
	SpecPath string
	Title    string
}

// Standard service endpoints. Empty DocsOptions use these paths so mounted
// Koba apps expose their contracts consistently.
const (
	DefaultDocsPath = "/api-doc"
	DefaultSpecPath = "/openapi.json"
)

// Mount generates a document from the APIs registered on app and exposes a
// self-contained Swagger UI plus the raw JSON contract on the app's Gin
// transport. Callers choose when to mount it relative to authentication
// middleware.
func Mount(app any, config Config, options DocsOptions) (*openapi3.T, error) {
	registry, ok := app.(fb.WithAPIDescriptions)
	if !ok {
		return nil, fmt.Errorf("koba/openapi: app does not expose registered APIs")
	}
	withRouter, ok := app.(fb.WithRouter)
	if !ok {
		return nil, fmt.Errorf("koba/openapi: app does not expose its Gin transport")
	}
	document, err := GenerateFromApp(config, registry)
	if err != nil {
		return nil, err
	}
	if err := RegisterDocs(withRouter.Router(), document, options); err != nil {
		return nil, err
	}
	return document, nil
}

// MountJSON exposes a generated or embedded OpenAPI JSON contract on the same
// HTTP transport as app. Empty DocsOptions mount the raw contract at
// DefaultSpecPath and Swagger UI at DefaultDocsPath.
func MountJSON(app any, contents []byte, options DocsOptions) (*openapi3.T, error) {
	withRouter, ok := app.(fb.WithRouter)
	if !ok {
		return nil, fmt.Errorf("koba/openapi: app does not expose its Gin transport")
	}
	return RegisterJSONDocs(withRouter.Router(), contents, options)
}

// RegisterDocs mounts an already-built document on a Gin router.
func RegisterDocs(router httprouter.IRouter, document *openapi3.T, options DocsOptions) error {
	if router == nil {
		return fmt.Errorf("koba/openapi: documentation router is nil")
	}
	if document == nil {
		return fmt.Errorf("koba/openapi: documentation document is nil")
	}
	docsPath, err := normalizeEndpointPath(options.DocsPath, DefaultDocsPath)
	if err != nil {
		return fmt.Errorf("koba/openapi: docs path: %w", err)
	}
	specPath, err := normalizeEndpointPath(options.SpecPath, DefaultSpecPath)
	if err != nil {
		return fmt.Errorf("koba/openapi: spec path: %w", err)
	}
	if docsPath == specPath {
		return fmt.Errorf("koba/openapi: docs and spec paths must differ")
	}
	docsPrefix := strings.TrimSuffix(docsPath, "/") + "/"
	if strings.HasPrefix(specPath, docsPrefix) {
		return fmt.Errorf("koba/openapi: spec path must not be nested below the docs path")
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("koba/openapi: encode runtime document: %w", err)
	}
	contents = append(contents, '\n')

	title := strings.TrimSpace(options.Title)
	if title == "" && document.Info != nil {
		title = strings.TrimSpace(document.Info.Title) + " documentation"
	}
	if title == "" {
		title = "API documentation"
	}
	publicDocsPath := publicRouterPath(router, docsPath)
	publicSpecPath := publicRouterPath(router, specPath)
	relativeSpec, err := filepath.Rel(strings.TrimSuffix(publicDocsPath, "/"), publicSpecPath)
	if err != nil {
		return fmt.Errorf("koba/openapi: resolve documentation spec URL: %w", err)
	}
	relativeSpec = filepath.ToSlash(relativeSpec)
	docsBase := strings.TrimSuffix(docsPath, "/") + "/"
	publicDocsBase := strings.TrimSuffix(publicDocsPath, "/") + "/"
	ui := v5emb.New(title, relativeSpec, publicDocsBase)

	router.GET(specPath, func(context *httprouter.Context) {
		context.Header("Cache-Control", "no-cache")
		context.Data(http.StatusOK, "application/json; charset=utf-8", contents)
	})
	router.GET(strings.TrimSuffix(docsBase, "/"), func(context *httprouter.Context) {
		context.Redirect(http.StatusPermanentRedirect, publicDocsBase)
	})
	router.GET(docsBase+"*asset", httprouter.WrapH(ui))
	return nil
}

// RegisterJSONDocs parses and validates a generated OpenAPI JSON document,
// then mounts it using the same endpoints and embedded Swagger UI as
// RegisterDocs. It is intended for applications which commit or embed their
// generated contract instead of rebuilding it through reflection at startup.
func RegisterJSONDocs(router httprouter.IRouter, contents []byte, options DocsOptions) (*openapi3.T, error) {
	if !json.Valid(contents) {
		return nil, fmt.Errorf("koba/openapi: parse static document: invalid JSON")
	}

	document, err := openapi3.NewLoader().LoadFromData(contents)
	if err != nil {
		return nil, fmt.Errorf("koba/openapi: parse static document: %w", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("koba/openapi: validate static document: %w", err)
	}
	if err := RegisterDocs(router, document, options); err != nil {
		return nil, err
	}
	return document, nil
}

type basePathRouter interface {
	BasePath() string
}

func publicRouterPath(router httprouter.IRouter, endpoint string) string {
	basePath := "/"
	if withBasePath, ok := router.(basePathRouter); ok {
		basePath = withBasePath.BasePath()
	}
	if basePath == "" || basePath == "/" {
		return endpoint
	}
	return path.Join(basePath, endpoint)
}

func normalizeEndpointPath(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("must start with /")
	}
	if strings.Contains(value, "?") || strings.Contains(value, "#") {
		return "", fmt.Errorf("must not contain a query or fragment")
	}
	value = path.Clean(value)
	if value == "/" {
		return "", fmt.Errorf("must not be the root path")
	}
	return value, nil
}
