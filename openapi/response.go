package openapi

import "github.com/getkin/kin-openapi/openapi3"

func errorResponse(description string) *openapi3.Response {
	return openapi3.NewResponse().
		WithDescription(description).
		WithJSONSchemaRef(&openapi3.SchemaRef{Ref: "#/components/schemas/ErrorResponse"})
}

func errorResponseSchema() *openapi3.Schema {
	status := openapi3.NewIntegerSchema()
	status.Description = "HTTP status code returned by the transport."
	errorName := openapi3.NewStringSchema()
	errorName.Description = "Lowercase HTTP status name with spaces replaced by underscores."
	message := openapi3.NewStringSchema()
	message.Description = "Human-readable explanation of the failure."

	schema := openapi3.NewObjectSchema().
		WithProperty("status", status).
		WithProperty("error", errorName).
		WithProperty("message", message).
		WithRequired([]string{"status", "error", "message"})
	schema.Description = "Standard error envelope emitted by the koba HTTP adapter."
	return schema
}
