package openapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	fb "github.com/roxiegrillo/koba/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/typepb"
)

type webSocketRequest struct {
	SessionID string
}

type webSocketRequestContract struct {
	SessionID fb.Binding[string]
}

type webSocketInput struct {
	Message string `json:"message"`
}

type webSocketOutput struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type webSocketFixtureAPI struct{}

type protobufClientMessageContract struct {
	Name fb.MessageValueBinding[string]
}

type protobufServerMessageContract struct {
	Name fb.MessageValueBinding[string]
}

type documentedProtobufSocketAPI struct{}

type sameTypeDirectionalSocketAPI struct{}

type multiOperationSocketAPI struct{}

func (*documentedProtobufSocketAPI) Connect(context.Context) {}

func (*documentedProtobufSocketAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Connect": {Route: "/documented-protobuf-socket"},
	}}
}

func (*documentedProtobufSocketAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Connect": {Transport: fb.WebSocket(fb.Socket[*typepb.Type, *typepb.Enum]().WithMessages(
			fb.ClientMessage("Logical protobuf message accepted from clients.", protobufClientMessageContract{
				Name: fb.MessageValue[string](fb.Required(), fb.MinLength(1)).Document(
					"Fully qualified client type name.",
					fb.Example("fenix.chat.ClientMessage"),
				),
			}),
			fb.ServerMessage("Logical protobuf message delivered by the server.", protobufServerMessageContract{
				Name: fb.MessageValue[string](fb.Required(), fb.WithFormat("protobuf-full-name")).Document(
					"Fully qualified server enum name.",
				),
			}),
		))},
	}}
}

func (*sameTypeDirectionalSocketAPI) Connect(context.Context) {}

func (*multiOperationSocketAPI) First(context.Context) {}

func (*multiOperationSocketAPI) Second(context.Context) {}

func (*multiOperationSocketAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		// These IDs deliberately sanitize to the same component fragment. The
		// encoded identity must still keep their message schemas independent.
		"First":  {Route: "/first", OperationID: "socket/first"},
		"Second": {Route: "/second", OperationID: "socket first"},
	}}
}

func (*multiOperationSocketAPI) APIContract() fb.APIContract {
	message := func(description, fieldDescription string) fb.TransportContract {
		return fb.WebSocket(fb.Socket[webSocketInput, webSocketOutput]().WithMessages(
			fb.ClientMessage(description, struct {
				Message fb.MessageValueBinding[string]
			}{
				Message: fb.MessageValue[string]().Document(fieldDescription),
			}),
			fb.ServerMessage("Shared server message", struct {
				Message fb.MessageValueBinding[string]
			}{
				Message: fb.MessageValue[string]().Document("Server message."),
			}),
		))
	}
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"First":  {Transport: message("First client message", "First message field.")},
		"Second": {Transport: message("Second client message", "Second message field.")},
	}}
}

func (*sameTypeDirectionalSocketAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Connect": {Route: "/same-type-socket"},
	}}
}

func (*sameTypeDirectionalSocketAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Connect": {Transport: fb.WebSocket(fb.Socket[*typepb.Type, *typepb.Type]().WithMessages(
			fb.ClientMessage("Client view of the shared protobuf type.", protobufClientMessageContract{
				Name: fb.MessageValue[string](fb.MinLength(2)).Document("Client name."),
			}),
			fb.ServerMessage("Server view of the shared protobuf type.", protobufClientMessageContract{
				Name: fb.MessageValue[string](fb.WithFormat("server-name")).Document("Server name."),
			}),
		))},
	}}
}

func (*webSocketFixtureAPI) Connect(context.Context, webSocketRequest) {}

func (*webSocketFixtureAPI) Routes() map[string]string {
	return map[string]string{"Connect": "/sessions/:session_id/connect"}
}

func (*webSocketFixtureAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name: "realtime",
		Operations: map[string]fb.OperationDocumentation{
			"Connect": {
				Method:      http.MethodGet,
				OperationID: "sessions_connect",
				Summary:     "Connect to a session",
				Description: "Opens the realtime session transport.",
				Security:    []fb.SecurityRequirement{{"bearerAuth"}},
				Responses: map[int]fb.ResponseDocumentation{
					http.StatusSwitchingProtocols: {Description: "Realtime connection established"},
				},
			},
		},
	}
}

func (*webSocketFixtureAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Connect": {
			Request: webSocketRequestContract{
				SessionID: fb.Path[string]("session_id", fb.WithFormat("uuid")),
			},
			Authorization: fb.AuthorizationContract{RequiredScopes: []string{"connect:sessions"}},
			Transport: fb.WebSocket(fb.Socket[
				*typepb.Type,
				*anypb.Any,
			]()),
		},
	}}
}

func TestGenerateInfersDirectionalProtobufWebSocketContract(t *testing.T) {
	customizedProtobuf := false
	document, err := Generate(Config{
		Title:   "WebSocket fixture",
		Version: "1.0.0",
		SecuritySchemes: []SecurityScheme{{
			Name:   "bearerAuth",
			Type:   SecurityTypeHTTP,
			Scheme: "bearer",
		}},
		SchemaCustomizer: func(context SchemaContext) error {
			if context.GoType != reflect.TypeFor[*typepb.Type]() {
				return nil
			}
			customizedProtobuf = true
			context.Schema.Properties["name"].Value.Format = "uuid"
			return nil
		},
		DisableDefaultProbes: true,
	}, (*webSocketFixtureAPI)(nil))
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}
	operation := document.Paths.Find("/sessions/{session_id}/connect").Get
	if operation == nil {
		t.Fatal("generated document is missing WebSocket GET")
	}
	if operation.Responses.Value("200") != nil || operation.Responses.Value("204") != nil {
		t.Fatalf("WebSocket responses retain normal success: %#v", operation.Responses.Map())
	}
	upgrade := operation.Responses.Value("101")
	if upgrade == nil || upgrade.Value == nil || upgrade.Value.Description == nil || *upgrade.Value.Description != "Realtime connection established" {
		t.Fatalf("WebSocket 101 response = %#v", upgrade)
	}
	assertWebSocketHeader(t, upgrade.Value, "Connection", "Upgrade")
	assertWebSocketHeader(t, upgrade.Value, "Upgrade", "websocket")

	extension, ok := operation.Extensions["x-websocket"].(map[string]any)
	if !ok {
		t.Fatalf("x-websocket = %#v", operation.Extensions["x-websocket"])
	}
	if extension["frameType"] != "binary" || extension["encoding"] != "protobuf" {
		t.Errorf("WebSocket frame contract = %#v", extension)
	}
	assertSocketSchemaReference(t, document, extension, "clientToServer")
	assertSocketSchemaReference(t, document, extension, "serverToClient")
	assertDirectionalProtobufMessage(t, extension, "clientToServer", "google.protobuf.Type")
	assertDirectionalProtobufMessage(t, extension, "serverToClient", "google.protobuf.Any")
	assertDescriptorSetResolves(t, extension["protobufDescriptorSet"], "google.protobuf.Type", "google.protobuf.Any")
	if !customizedProtobuf {
		t.Fatal("SchemaCustomizer was not called for the protobuf Go type")
	}
	clientReference := extension["clientToServer"].(map[string]any)["$ref"].(string)
	clientComponent := document.Components.Schemas[strings.TrimPrefix(clientReference, "#/components/schemas/")]
	if clientComponent == nil || clientComponent.Value == nil || clientComponent.Value.Properties["name"].Value.Format != "uuid" {
		t.Fatalf("customized protobuf component = %#v", clientComponent)
	}

	if got := operation.Extensions["x-required-scopes"]; !reflect.DeepEqual(got, []string{"connect:sessions"}) {
		t.Errorf("x-required-scopes = %#v", got)
	}
	if !strings.Contains(operation.Description, "Required OAuth scopes: connect:sessions.") {
		t.Errorf("operation description does not expose required scopes: %q", operation.Description)
	}
}

func TestBuildProjectsDirectionalSocketMessageContractsWithoutSchemaCustomizer(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*documentedProtobufSocketAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Socket", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	operation := document.Paths.Find("/documented-protobuf-socket").Get
	extension, ok := operation.Extensions["x-websocket"].(map[string]any)
	if !ok {
		t.Fatalf("x-websocket = %#v", operation.Extensions["x-websocket"])
	}
	client := extension["clientToServer"].(map[string]any)
	if _, exists := client["description"]; exists {
		t.Fatalf("client direction contains non-canonical description field: %#v", client)
	}
	clientReference := client["$ref"].(string)
	clientSchema := document.Components.Schemas[strings.TrimPrefix(clientReference, "#/components/schemas/")].Value
	if clientSchema.Description != "Logical protobuf message accepted from clients." ||
		!containsString(clientSchema.Required, "name") {
		t.Fatalf("client schema = %#v", clientSchema)
	}
	name := clientSchema.Properties["name"].Value
	if name.Description != "Fully qualified client type name." || name.MinLength != 1 ||
		name.Example != "fenix.chat.ClientMessage" {
		t.Fatalf("client name schema = %#v", name)
	}

	server := extension["serverToClient"].(map[string]any)
	if _, exists := server["description"]; exists {
		t.Fatalf("server direction contains non-canonical description field: %#v", server)
	}
	serverReference := server["$ref"].(string)
	serverSchema := document.Components.Schemas[strings.TrimPrefix(serverReference, "#/components/schemas/")].Value
	if serverSchema.Description != "Logical protobuf message delivered by the server." ||
		!containsString(serverSchema.Required, "name") ||
		serverSchema.Properties["name"].Value.Format != "protobuf-full-name" {
		t.Fatalf("server schema = %#v", serverSchema)
	}
}

func TestBuildIsolatesSocketMessageMetadataPerOperation(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*multiOperationSocketAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Socket", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}

	firstExtension := document.Paths.Find("/first").Get.Extensions["x-websocket"].(map[string]any)
	secondExtension := document.Paths.Find("/second").Get.Extensions["x-websocket"].(map[string]any)
	firstRef := firstExtension["clientToServer"].(map[string]any)["$ref"].(string)
	secondRef := secondExtension["clientToServer"].(map[string]any)["$ref"].(string)
	if firstRef == secondRef {
		t.Fatalf("operations share client schema %q", firstRef)
	}
	first := document.Components.Schemas[strings.TrimPrefix(firstRef, "#/components/schemas/")].Value
	second := document.Components.Schemas[strings.TrimPrefix(secondRef, "#/components/schemas/")].Value
	if first.Description != "First client message" ||
		first.Properties["message"].Value.Description != "First message field." {
		t.Fatalf(
			"first client schema descriptions = root %q, message %q",
			first.Description,
			first.Properties["message"].Value.Description,
		)
	}
	if second.Description != "Second client message" ||
		second.Properties["message"].Value.Description != "Second message field." {
		t.Fatalf("second client schema = %#v", second)
	}
}

func TestBuildKeepsSameTypeDirectionalSocketSchemasIndependent(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*sameTypeDirectionalSocketAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Socket", Version: "1"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	extension := document.Paths.Find("/same-type-socket").Get.Extensions["x-websocket"].(map[string]any)
	clientRef := extension["clientToServer"].(map[string]any)["$ref"].(string)
	serverRef := extension["serverToClient"].(map[string]any)["$ref"].(string)
	if clientRef == serverRef {
		t.Fatalf("same-type directions share schema %q", clientRef)
	}
	client := document.Components.Schemas[strings.TrimPrefix(clientRef, "#/components/schemas/")].Value
	server := document.Components.Schemas[strings.TrimPrefix(serverRef, "#/components/schemas/")].Value
	if client.Description != "Client view of the shared protobuf type." ||
		client.Properties["name"].Value.Description != "Client name." ||
		client.Properties["name"].Value.MinLength != 2 ||
		client.Properties["name"].Value.Format != "" {
		t.Fatalf("client directional schema = %#v", client)
	}
	if server.Description != "Server view of the shared protobuf type." ||
		server.Properties["name"].Value.Description != "Server name." ||
		server.Properties["name"].Value.Format != "server-name" ||
		server.Properties["name"].Value.MinLength != 0 {
		t.Fatalf("server directional schema = %#v", server)
	}
}

func TestBuildDocumentsTextJSONWebSocketContract(t *testing.T) {
	document, err := Build(Config{Title: "JSON socket", Version: "1.0.0"}, []fb.APIDescription{{
		Name: "realtime",
		Operations: []fb.OperationDescription{{
			MethodName:  "Connect",
			OperationID: "json_connect",
			Route:       "/connect",
			HTTPMethod:  http.MethodGet,
			Contract: fb.OperationContract{Transport: fb.WebSocket(fb.Socket[
				webSocketInput,
				webSocketOutput,
			]())},
		}},
	}})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	extension := document.Paths.Find("/connect").Get.Extensions["x-websocket"].(map[string]any)
	if extension["frameType"] != "text" || extension["encoding"] != "json" {
		t.Fatalf("JSON WebSocket extension = %#v", extension)
	}
	if _, exists := extension["protobufMessage"]; exists {
		t.Errorf("JSON WebSocket unexpectedly has protobufMessage: %#v", extension)
	}
	if _, exists := extension["protobufDescriptorSet"]; exists {
		t.Errorf("JSON WebSocket unexpectedly has protobufDescriptorSet: %#v", extension)
	}
}

func assertWebSocketHeader(t *testing.T, response *openapi3.Response, name, enum string) {
	t.Helper()
	header := response.Headers[name]
	if header == nil || header.Value == nil || header.Value.Schema == nil || header.Value.Schema.Value == nil {
		t.Fatalf("WebSocket response header %s = %#v", name, header)
	}
	if got, want := header.Value.Schema.Value.Enum, []any{enum}; !reflect.DeepEqual(got, want) {
		t.Errorf("WebSocket response header %s enum = %#v, want %#v", name, got, want)
	}
}

func assertSocketSchemaReference(t *testing.T, document *openapi3.T, extension map[string]any, name string) {
	t.Helper()
	direction, ok := extension[name].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v", name, extension[name])
	}
	reference, ok := direction["$ref"].(string)
	if !ok || !strings.HasPrefix(reference, "#/components/schemas/") {
		t.Fatalf("%s reference = %#v", name, direction["$ref"])
	}
	component := strings.TrimPrefix(reference, "#/components/schemas/")
	if document.Components.Schemas[component] == nil {
		t.Errorf("%s references missing component %q", name, component)
	}
}

func assertDirectionalProtobufMessage(t *testing.T, extension map[string]any, direction string, want string) {
	t.Helper()
	value, ok := extension[direction].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v", direction, extension[direction])
	}
	if got := value["protobufMessage"]; got != want {
		t.Errorf("%s protobufMessage = %#v, want %q", direction, got, want)
	}
}

func assertDescriptorSetResolves(t *testing.T, encoded any, fullNames ...protoreflect.FullName) {
	t.Helper()
	value, ok := encoded.(string)
	if !ok || value == "" {
		t.Fatalf("protobufDescriptorSet = %#v", encoded)
	}
	serialized, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode protobufDescriptorSet: %v", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(serialized, set); err != nil {
		t.Fatalf("unmarshal protobufDescriptorSet: %v", err)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		t.Fatalf("load protobufDescriptorSet: %v", err)
	}
	for _, fullName := range fullNames {
		if _, err := files.FindDescriptorByName(fullName); err != nil {
			t.Fatalf("protobufDescriptorSet does not resolve %s: %v", fullName, err)
		}
	}
}
