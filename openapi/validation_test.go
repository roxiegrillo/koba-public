package openapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
)

func TestGenerateRejectsDuplicateAndInvalidOperationMetadata(t *testing.T) {
	t.Run("normalizes a direct description HTTP method", func(t *testing.T) {
		document, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:  "Get",
				OperationID: "getWidget",
				Route:       "/widgets",
				HTTPMethod:  " get ",
			}},
		}})
		if err != nil {
			t.Fatalf("Build() returned an error: %v", err)
		}
		if operation := document.Paths.Find("/widgets").Get; operation == nil || operation.OperationID != "getWidget" {
			t.Fatalf("normalized GET operation = %#v", operation)
		}
	})

	t.Run("duplicate parameter", func(t *testing.T) {
		description, err := fb.DescribeAPI((*contractFixtureAPI)(nil))
		if err != nil {
			t.Fatalf("DescribeAPI() returned an error: %v", err)
		}
		description.Operations[0].Documentation.Parameters = append(
			description.Operations[0].Documentation.Parameters,
			fb.ParameterOf[int]("id", fb.ParameterInPath, "Duplicate widget ID", true),
		)
		_, err = Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{description})
		if err == nil || !strings.Contains(err.Error(), "duplicate parameter") {
			t.Fatalf("Build() error = %v, want duplicate parameter error", err)
		}
	})

	t.Run("unknown security scheme", func(t *testing.T) {
		_, err := Generate(Config{Title: "Widgets", Version: "1"}, (*contractFixtureAPI)(nil))
		if err == nil || !strings.Contains(err.Error(), "unknown security scheme") {
			t.Fatalf("Generate() error = %v, want unknown security scheme error", err)
		}
	})

	t.Run("unsupported success status", func(t *testing.T) {
		description, err := fb.DescribeAPI((*contractFixtureAPI)(nil))
		if err != nil {
			t.Fatalf("DescribeAPI() returned an error: %v", err)
		}
		description.Operations[0].Documentation.Responses[http.StatusCreated] = fb.ResponseDocumentation{
			Description: "Created",
		}
		_, err = Build(Config{
			Title:   "Widgets",
			Version: "1",
			SecuritySchemes: []SecurityScheme{{
				Name:      "tokenAuth",
				Type:      SecurityTypeAPIKey,
				In:        openapi3.ParameterInHeader,
				Parameter: "Authorization",
			}},
		}, []fb.APIDescription{description})
		if err == nil || !strings.Contains(err.Error(), "resolved success status") {
			t.Fatalf("Build() error = %v, want unsupported success status error", err)
		}
	})

	t.Run("unsupported HTTP method", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:  "Trace",
				OperationID: "traceWidget",
				Route:       "/widgets",
				HTTPMethod:  http.MethodTrace,
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "supported methods are GET, POST, PUT, PATCH, and DELETE") {
			t.Fatalf("Build() error = %v, want unsupported HTTP method error", err)
		}
	})

	t.Run("direct description rejects non-2xx success", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:    "Get",
				OperationID:   "invalidStatusGet",
				Route:         "/widgets",
				HTTPMethod:    http.MethodGet,
				Documentation: fb.OperationDocumentation{SuccessStatus: 300},
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "between 200 and 299") {
			t.Fatalf("Build() error = %v, want invalid success status error", err)
		}
	})

	t.Run("direct description rejects 204 with response", func(t *testing.T) {
		_, err := Build(Config{Title: "Widgets", Version: "1"}, []fb.APIDescription{{
			Operations: []fb.OperationDescription{{
				MethodName:    "Get",
				OperationID:   "invalidNoContentGet",
				Route:         "/widgets",
				HTTPMethod:    http.MethodGet,
				OutputType:    reflect.TypeFor[ContractFixtureResponse](),
				Documentation: fb.OperationDocumentation{SuccessStatus: http.StatusNoContent},
			}},
		}})
		if err == nil || !strings.Contains(err.Error(), "cannot be used by an operation with a response value") {
			t.Fatalf("Build() error = %v, want body-forbidden status error", err)
		}
	})
}
