package openapi

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	fb "github.com/roxiegrillo/koba/v2"
	internalprotobufdescriptor "github.com/roxiegrillo/koba/v2/internal/protobufdescriptor"
	"google.golang.org/protobuf/proto"
)

func renderWebSocketExtension(
	generator *openapi3gen.Generator,
	schemas openapi3.Schemas,
	customizer SchemaCustomizer,
	contract fb.WebSocketTransportContract,
	operationID string,
) (map[string]any, error) {
	client, err := socketSchemaReference(
		generator,
		schemas,
		customizer,
		contract.Socket.ClientToServerType,
		contract.Socket.ClientToServer,
		operationID,
		"ClientToServer",
	)
	if err != nil {
		return nil, fmt.Errorf("client-to-server schema: %w", err)
	}
	serverCustomizer := customizer
	if contract.Socket.ServerToClientType == contract.Socket.ClientToServerType {
		serverCustomizer = nil
	}
	server, err := socketSchemaReference(
		generator,
		schemas,
		serverCustomizer,
		contract.Socket.ServerToClientType,
		contract.Socket.ServerToClient,
		operationID,
		"ServerToClient",
	)
	if err != nil {
		return nil, fmt.Errorf("server-to-client schema: %w", err)
	}
	// api-cli's directional extension contract accepts only the schema reference
	// and wire-specific metadata. Message descriptions live on the referenced,
	// operation-local schemas created by socketSchemaReference.
	clientDirection := map[string]any{"$ref": client}
	serverDirection := map[string]any{"$ref": server}
	extension := map[string]any{
		"frameType":      string(contract.ResolvedFrameType()),
		"encoding":       string(contract.ResolvedEncoding()),
		"clientToServer": clientDirection,
		"serverToClient": serverDirection,
	}
	if contract.ResolvedEncoding() == fb.WebSocketEncodingProtobuf {
		description, err := internalprotobufdescriptor.DescribeSet(
			contract.Socket.ClientToServerType,
			contract.Socket.ServerToClientType,
		)
		if err != nil {
			return nil, err
		}
		clientDirection["protobufMessage"] = description.FullNames[0]
		serverDirection["protobufMessage"] = description.FullNames[1]
		extension["protobufDescriptorSet"] = description.Base64DescriptorSet
	}
	return extension, nil
}

func socketSchemaReference(
	generator *openapi3gen.Generator,
	schemas openapi3.Schemas,
	customizer SchemaCustomizer,
	valueType reflect.Type,
	message *fb.SocketMessageContract,
	operationID string,
	direction string,
) (string, error) {
	var (
		reference string
		err       error
	)
	if valueType.Implements(reflect.TypeFor[proto.Message]()) {
		reference, err = protobufSocketSchemaReference(schemas, customizer, valueType)
	} else {
		var ref *openapi3.SchemaRef
		ref, err = schemaForType(generator, schemas, valueType)
		if err == nil {
			if ref.Ref != "" {
				reference = ref.Ref
			} else {
				name := componentName(valueType)
				if name == "" {
					return "", fmt.Errorf("socket schema type %s has no component name", valueType)
				}
				if existing := schemas[name]; existing != nil && existing != ref {
					return "", fmt.Errorf("socket schema component %q conflicts with another Go type", name)
				}
				schemas[name] = ref
				reference = "#/components/schemas/" + name
			}
		}
	}
	if err != nil {
		return "", err
	}
	if message == nil {
		return reference, nil
	}
	const componentPrefix = "#/components/schemas/"
	if !strings.HasPrefix(reference, componentPrefix) {
		return "", fmt.Errorf("socket schema reference %q is not a component", reference)
	}
	name := strings.TrimPrefix(reference, componentPrefix)
	component := schemas[name]
	if component == nil {
		return "", fmt.Errorf("socket schema component %q was not registered", name)
	}
	local, err := socketMessageContractSchema(component, valueType, message.Bindings, schemas)
	if err != nil {
		return "", err
	}
	local.Value.Description = message.Description
	operationComponent := strings.Trim(
		componentCharacter.ReplaceAllString(operationID, "_"),
		"_",
	)
	if operationComponent == "" {
		return "", fmt.Errorf("socket operation ID %q has no component-safe characters", operationID)
	}
	// Message contracts belong to one operation and direction, not to the Go
	// type globally. Always project them into an independent component so a
	// second socket or an HTTP use of the same type cannot overwrite metadata.
	operationIdentity := base64.RawURLEncoding.EncodeToString([]byte(operationID))
	directionalName := name + "." + operationComponent + "." + operationIdentity + "." + direction
	schemas[directionalName] = local
	return "#/components/schemas/" + directionalName, nil
}

func socketMessageContractSchema(
	ref *openapi3.SchemaRef,
	messageType reflect.Type,
	bindings []fb.ResponseBinding,
	schemas openapi3.Schemas,
) (*openapi3.SchemaRef, error) {
	root, err := mutableSchemaRef(ref, schemas)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		if len(binding.FieldPath) == 0 {
			if responseBindingEntersElement(binding.ElementAt, 0) {
				if root.Value.Items == nil {
					return nil, fmt.Errorf("root socket message list has no item schema")
				}
				item, itemErr := mutableSchemaRef(root.Value.Items, schemas)
				if itemErr != nil {
					return nil, fmt.Errorf("root socket message list item: %w", itemErr)
				}
				root.Value.Items, itemErr = documentedInputContractSchema(
					item,
					binding.Input,
					binding.Documentation,
					schemas,
				)
				if itemErr != nil {
					return nil, fmt.Errorf("root socket message list item: %w", itemErr)
				}
			} else {
				root, err = documentedInputContractSchema(root, binding.Input, binding.Documentation, schemas)
				if err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := applyResponseValueContract(root, messageType, binding, schemas); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func webSocketUpgradeResponse() *openapi3.Response {
	response := openapi3.NewResponse().WithDescription("WebSocket connection established")
	setWebSocketUpgradeHeaders(response)
	return response
}

func setWebSocketUpgradeHeaders(response *openapi3.Response) {
	if response.Headers == nil {
		response.Headers = make(openapi3.Headers, 2)
	}
	response.Headers["Connection"] = webSocketUpgradeHeader(
		"Upgrade confirmation.",
		"Upgrade",
	)
	response.Headers["Upgrade"] = webSocketUpgradeHeader(
		"Selected upgraded protocol.",
		"websocket",
	)
}

func webSocketUpgradeHeader(description, value string) *openapi3.HeaderRef {
	header := &openapi3.Header{Parameter: openapi3.Parameter{
		Description: description,
		Schema:      &openapi3.SchemaRef{Value: openapi3.NewStringSchema().WithEnum(value)},
	}}
	return &openapi3.HeaderRef{Value: header}
}
