package koba

import "reflect"

// ParameterLocation identifies the HTTP location from which an operation reads
// a parameter not represented by its JSON request-body argument.
type ParameterLocation string

const (
	ParameterInQuery  ParameterLocation = "query"
	ParameterInHeader ParameterLocation = "header"
	ParameterInCookie ParameterLocation = "cookie"
	ParameterInPath   ParameterLocation = "path"
)

// ParameterDocumentation describes an HTTP parameter and its OpenAPI prose.
// Request descriptors normally own its binding, type, and structural rules.
type ParameterDocumentation struct {
	Name string
	In   ParameterLocation
	// ContractInput supports the legacy semantic-input/context binding model.
	// New APIs should bind parameters through OperationContract.Request.
	ContractInput string
	Description   string
	Required      bool
	Deprecated    bool
	GoType        reflect.Type
	Format        string
	Pattern       string
	Enum          []any
	Example       any
}

// ParameterOf creates typed parameter metadata without requiring callers to
// import reflect.
func ParameterOf[T any](name string, in ParameterLocation, description string, required bool) ParameterDocumentation {
	return ParameterDocumentation{
		Name:        name,
		In:          in,
		Description: description,
		Required:    required,
		GoType:      reflect.TypeFor[T](),
	}
}

// ResponseDocumentation documents a response which cannot be inferred from
// the function's success return type, normally a domain error response.
type ResponseDocumentation struct {
	Description string
	Headers     map[string]HeaderDocumentation
}

// HeaderDocumentation describes an HTTP response header. Response header
// values are projected as strings; structural value constraints belong in
// APIContract, not in HTTP documentation.
type HeaderDocumentation struct {
	Description string
	Required    bool
	Format      string
	Pattern     string
	Enum        []string
	Example     string
}

// SecurityRequirement is one authentication alternative. Every named scheme
// in one requirement is required together (OpenAPI AND semantics). A slice of
// requirements represents alternatives (OpenAPI OR semantics).
type SecurityRequirement []string

// OperationDocumentation contains HTTP and OpenAPI metadata keyed by Go method
// name in APIDocumentation. Structural input rules belong in APIContract.
type OperationDocumentation struct {
	// Method overrides the HTTP method inferred from the Go signature.
	// Supported values are GET, POST, PUT, PATCH, and DELETE.
	Method string
	// SuccessStatus overrides the inferred success status. Operations which
	// return a value default to 200; void operations default to 204.
	SuccessStatus          int
	OperationID            string
	Summary                string
	Description            string
	Tags                   []string
	Parameters             []ParameterDocumentation
	RequestBodyDescription string
	Responses              map[int]ResponseDocumentation
	Deprecated             bool
	// RequiredScopes is the compatibility location for APIs authored before
	// authorization became part of APIContract. New APIs should declare scopes
	// in OperationContract.Authorization. Defining both is invalid.
	RequiredScopes []string
	// Security overrides the document-level security requirements when it is
	// non-nil. Items inside one requirement are combined with AND; multiple
	// requirements are alternatives combined with OR.
	Security []SecurityRequirement
	// Public explicitly disables document-level security for this operation.
	Public bool
}

// APIDocumentation is optional HTTP/OpenAPI documentation supplied by an API
// value. Operations is keyed by exported Go method name.
type APIDocumentation struct {
	Name        string
	Description string
	Operations  map[string]OperationDocumentation
}

// WithAPIDocumentation is implemented by API values that want richer generated
// documentation than reflection alone can provide. Implementations should be
// metadata-only so the method can safely be called on a typed nil receiver by
// build-time generators.
type WithAPIDocumentation interface {
	APIDocumentation() APIDocumentation
}
