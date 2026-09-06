package koba

import (
	"fmt"
	"reflect"
	"strings"

	internalprotobufdescriptor "github.com/roxiegrillo/koba/v2/internal/protobufdescriptor"
	"google.golang.org/protobuf/proto"
)

// TransportContract selects the external adapter which invokes one operation.
// A zero value is an ordinary HTTP request/response operation. WebSocket is an
// HTTP upgrade, while KafkaConsumer is a message-driven invocation with no
// HTTP route.
type TransportContract struct {
	WebSocket     *WebSocketTransportContract
	KafkaConsumer *KafkaConsumerTransportContract
}

// KafkaConsumerTransportContract binds a typed API function to one Kafka
// topic. Broker addresses, consumer groups, credentials, retries, and concrete
// readers are runtime configuration and deliberately remain outside the
// functional contract.
type KafkaConsumerTransportContract struct {
	Topic string
}

// KafkaMessageEncoding is inferred from the function input type. Protobuf
// messages use the protobuf wire format; ordinary named structs use JSON.
type KafkaMessageEncoding string

const (
	KafkaMessageEncodingProtobuf KafkaMessageEncoding = "protobuf"
	KafkaMessageEncodingJSON     KafkaMessageEncoding = "json"
)

// SocketContract describes the application protocol carried after a
// connection has been established. Its types are directional: ClientToServer
// is received by the API and ServerToClient is sent by the API.
//
// API authors normally construct this value with Socket[ClientToServer,
// ServerToClient] rather than setting the reflected types directly.
type SocketContract struct {
	ClientToServerType reflect.Type
	ServerToClientType reflect.Type
	// ClientToServer and ServerToClient optionally describe the logical schema
	// carried in each direction. Their descriptors are compiled into
	// framework-owned bindings during API description so static plans do not
	// retain application companion values.
	ClientToServer *SocketMessageContract
	ServerToClient *SocketMessageContract
}

// SocketMessageContract describes one directional message schema without
// coupling API authors to WebSocket framing, JSON, protobuf, or OpenAPI.
// Descriptor is an authoring-time partial companion whose leaves are
// MessageValue values. Bindings is the compiled representation used by static
// plans and renderers; authors should leave it empty.
type SocketMessageContract struct {
	Description string
	Descriptor  any
	Bindings    []ResponseBinding
}

// SocketOption adds optional directional schema metadata to a SocketContract.
type SocketOption interface {
	applySocket(*SocketContract)
}

type socketOptionFunc func(*SocketContract)

func (option socketOptionFunc) applySocket(socket *SocketContract) { option(socket) }

// WebSocketTransportContract describes an HTTP operation whose successful
// handshake establishes a bidirectional WebSocket carrying Socket messages.
type WebSocketTransportContract struct {
	Socket SocketContract
}

// WebSocketFrameType identifies the WebSocket frame representation on the
// wire. It is inferred from the directional socket types.
type WebSocketFrameType string

const (
	// WebSocketFrameBinary carries binary WebSocket data frames.
	WebSocketFrameBinary WebSocketFrameType = "binary"
	// WebSocketFrameText carries text WebSocket data frames.
	WebSocketFrameText WebSocketFrameType = "text"
)

// WebSocketEncoding identifies the application payload encoding inside each
// WebSocket frame. It is inferred from the directional socket types.
type WebSocketEncoding string

const (
	// WebSocketEncodingProtobuf carries serialized Protocol Buffer messages.
	WebSocketEncodingProtobuf WebSocketEncoding = "protobuf"
	// WebSocketEncodingJSON carries JSON documents.
	WebSocketEncodingJSON WebSocketEncoding = "json"
)

var protobufMessageType = reflect.TypeFor[proto.Message]()

// Socket declares the messages exchanged inside a connection. The types are
// the single source of truth for both runtime framing and generated contracts:
// two protobuf messages infer binary/protobuf, while two ordinary named
// structs infer text/json.
func Socket[ClientToServer any, ServerToClient any]() SocketContract {
	return SocketContract{
		ClientToServerType: reflect.TypeFor[ClientToServer](),
		ServerToClientType: reflect.TypeFor[ServerToClient](),
	}
}

// WithMessages attaches directional schema metadata while preserving the
// original v1 Socket constructor signature.
func (socket SocketContract) WithMessages(options ...SocketOption) SocketContract {
	for _, option := range options {
		if option != nil {
			option.applySocket(&socket)
		}
	}
	return socket
}

// ClientMessage documents and structurally describes values received from a
// connected client. descriptor mirrors the declared ClientToServer type.
func ClientMessage(description string, descriptor any) SocketOption {
	return socketOptionFunc(func(socket *SocketContract) {
		socket.ClientToServer = &SocketMessageContract{
			Description: description,
			Descriptor:  descriptor,
		}
	})
}

// ServerMessage documents and structurally describes values sent to a
// connected client. descriptor mirrors the declared ServerToClient type.
func ServerMessage(description string, descriptor any) SocketOption {
	return socketOptionFunc(func(socket *SocketContract) {
		socket.ServerToClient = &SocketMessageContract{
			Description: description,
			Descriptor:  descriptor,
		}
	})
}

// WebSocket declares that an operation's successful HTTP handshake establishes
// the supplied socket protocol.
func WebSocket(socket SocketContract) TransportContract {
	return TransportContract{WebSocket: &WebSocketTransportContract{Socket: socket}}
}

// KafkaConsumer declares that Kafka messages from topic invoke the operation.
// The operation input type is the single source of truth for the payload type
// and encoding.
func KafkaConsumer(topic string) TransportContract {
	return TransportContract{KafkaConsumer: &KafkaConsumerTransportContract{Topic: topic}}
}

// ResolvedFrameType returns the frame type inferred from the socket message
// types. Contract normalization guarantees this method succeeds.
func (contract WebSocketTransportContract) ResolvedFrameType() WebSocketFrameType {
	frameType, _, _ := inferSocketRepresentation(contract.Socket)
	return frameType
}

// ResolvedEncoding returns the payload encoding inferred from the socket
// message types. Contract normalization guarantees this method succeeds.
func (contract WebSocketTransportContract) ResolvedEncoding() WebSocketEncoding {
	_, encoding, _ := inferSocketRepresentation(contract.Socket)
	return encoding
}

func normalizeTransportContract(contract TransportContract) (TransportContract, error) {
	if contract.WebSocket != nil && contract.KafkaConsumer != nil {
		return TransportContract{}, fmt.Errorf("an operation cannot use both WebSocket and Kafka consumer transports")
	}
	if contract.KafkaConsumer != nil {
		consumer := *contract.KafkaConsumer
		consumer.Topic = strings.TrimSpace(consumer.Topic)
		if consumer.Topic == "" {
			return TransportContract{}, fmt.Errorf("Kafka consumer topic is required")
		}
		if consumer.Topic == "." || consumer.Topic == ".." {
			return TransportContract{}, fmt.Errorf("Kafka consumer topic %q is reserved", consumer.Topic)
		}
		if len(consumer.Topic) > 249 {
			return TransportContract{}, fmt.Errorf("Kafka consumer topic exceeds 249 characters")
		}
		for _, character := range consumer.Topic {
			if (character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') ||
				character == '.' || character == '_' || character == '-' {
				continue
			}
			return TransportContract{}, fmt.Errorf(
				"Kafka consumer topic %q contains unsupported character %q",
				consumer.Topic,
				character,
			)
		}
		return TransportContract{KafkaConsumer: &consumer}, nil
	}
	if contract.WebSocket == nil {
		return TransportContract{}, nil
	}
	webSocket := *contract.WebSocket
	if _, _, err := inferSocketRepresentation(webSocket.Socket); err != nil {
		return TransportContract{}, err
	}
	client, err := normalizeSocketMessageContract(
		"client-to-server",
		webSocket.Socket.ClientToServer,
		webSocket.Socket.ClientToServerType,
	)
	if err != nil {
		return TransportContract{}, err
	}
	server, err := normalizeSocketMessageContract(
		"server-to-client",
		webSocket.Socket.ServerToClient,
		webSocket.Socket.ServerToClientType,
	)
	if err != nil {
		return TransportContract{}, err
	}
	webSocket.Socket.ClientToServer = client
	webSocket.Socket.ServerToClient = server
	return TransportContract{WebSocket: &webSocket}, nil
}

func inferKafkaMessageEncoding(messageType reflect.Type) (KafkaMessageEncoding, error) {
	if messageType == nil {
		return "", fmt.Errorf("Kafka consumer message Go type is required")
	}
	underlying := messageType
	if underlying.Kind() == reflect.Pointer {
		underlying = underlying.Elem()
	}
	if underlying.Kind() == reflect.Pointer {
		return "", fmt.Errorf(
			"Kafka consumer message Go type must be a named struct or pointer to one, got %s",
			messageType,
		)
	}
	if underlying.Kind() != reflect.Struct || underlying.Name() == "" || underlying.PkgPath() == "" {
		return "", fmt.Errorf(
			"Kafka consumer message Go type must be a named struct or pointer to one, got %s",
			messageType,
		)
	}
	if messageType.Implements(protobufMessageType) ||
		(messageType.Kind() != reflect.Pointer && reflect.PointerTo(messageType).Implements(protobufMessageType)) {
		return KafkaMessageEncodingProtobuf, nil
	}
	return KafkaMessageEncodingJSON, nil
}

func normalizeSocketMessageContract(
	direction string,
	contract *SocketMessageContract,
	messageType reflect.Type,
) (*SocketMessageContract, error) {
	if contract == nil {
		return nil, nil
	}
	result := *contract
	result.Description = strings.TrimSpace(contract.Description)
	if result.Description == "" {
		return nil, fmt.Errorf("WebSocket %s message description is required", direction)
	}
	if contract.Descriptor != nil && contract.Bindings != nil {
		return nil, fmt.Errorf(
			"WebSocket %s message cannot contain both an authoring descriptor and compiled bindings",
			direction,
		)
	}
	var (
		bindings []ResponseBinding
		err      error
	)
	if contract.Descriptor != nil {
		bindings, err = normalizeResponseBindings(contract.Descriptor, messageType)
	} else if contract.Bindings != nil {
		bindings, err = normalizeResolvedResponseBindings(contract.Bindings, messageType)
	}
	if err != nil {
		return nil, fmt.Errorf("WebSocket %s message: %w", direction, err)
	}
	result.Descriptor = nil
	result.Bindings = bindings
	return &result, nil
}

func inferSocketRepresentation(socket SocketContract) (WebSocketFrameType, WebSocketEncoding, error) {
	clientProtobuf, err := classifySocketType("client-to-server", socket.ClientToServerType)
	if err != nil {
		return "", "", err
	}
	serverProtobuf, err := classifySocketType("server-to-client", socket.ServerToClientType)
	if err != nil {
		return "", "", err
	}
	if clientProtobuf != serverProtobuf {
		return "", "", fmt.Errorf(
			"WebSocket socket message types cannot mix protobuf and JSON: client-to-server is %s while server-to-client is %s",
			socketRepresentationName(clientProtobuf),
			socketRepresentationName(serverProtobuf),
		)
	}
	if !clientProtobuf {
		return WebSocketFrameText, WebSocketEncodingJSON, nil
	}
	if _, err := internalprotobufdescriptor.DescribeSet(
		socket.ClientToServerType,
		socket.ServerToClientType,
	); err != nil {
		return "", "", err
	}
	return WebSocketFrameBinary, WebSocketEncodingProtobuf, nil
}

func classifySocketType(direction string, valueType reflect.Type) (bool, error) {
	if valueType == nil {
		return false, fmt.Errorf("WebSocket %s message Go type is required", direction)
	}
	if valueType.Implements(protobufMessageType) {
		return true, nil
	}
	underlying := valueType
	for underlying.Kind() == reflect.Pointer {
		underlying = underlying.Elem()
	}
	if underlying.Kind() != reflect.Struct || underlying.Name() == "" || underlying.PkgPath() == "" {
		return false, fmt.Errorf(
			"WebSocket %s JSON message Go type must be a named struct or pointer to one, got %s",
			direction,
			valueType,
		)
	}
	return false, nil
}

func socketRepresentationName(protobuf bool) string {
	if protobuf {
		return "protobuf"
	}
	return "JSON"
}
