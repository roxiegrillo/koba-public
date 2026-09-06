package koba

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/typepb"
)

type webSocketClientFixture struct {
	Message string `json:"message"`
}

type webSocketServerFixture struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type protobufTypeMessageContract struct {
	Name MessageValueBinding[string]
}

type protobufEnumMessageContract struct {
	Name MessageValueBinding[string]
}

func TestWebSocketSocketTypesInferWireRepresentation(t *testing.T) {
	protobuf := WebSocket(Socket[*typepb.Type, *anypb.Any]())
	if protobuf.WebSocket == nil {
		t.Fatal("WebSocket() returned no WebSocket contract")
	}
	if got, want := protobuf.WebSocket.ResolvedFrameType(), WebSocketFrameBinary; got != want {
		t.Errorf("protobuf frame type = %q, want %q", got, want)
	}
	if got, want := protobuf.WebSocket.ResolvedEncoding(), WebSocketEncodingProtobuf; got != want {
		t.Errorf("protobuf encoding = %q, want %q", got, want)
	}
	if got, want := protobuf.WebSocket.Socket.ClientToServerType, reflect.TypeFor[*typepb.Type](); got != want {
		t.Errorf("client message type = %v, want %v", got, want)
	}
	if got, want := protobuf.WebSocket.Socket.ServerToClientType, reflect.TypeFor[*anypb.Any](); got != want {
		t.Errorf("server message type = %v, want %v", got, want)
	}

	json := WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())
	if json.WebSocket == nil || json.WebSocket.ResolvedFrameType() != WebSocketFrameText || json.WebSocket.ResolvedEncoding() != WebSocketEncodingJSON {
		t.Fatalf("JSON WebSocket = %#v", json.WebSocket)
	}
}

func TestNormalizeOperationContractCopiesWebSocketMetadata(t *testing.T) {
	original := WebSocket(Socket[*typepb.Type, *anypb.Any]())
	normalized, err := normalizeOperationContract(OperationContract{Transport: original})
	if err != nil {
		t.Fatalf("normalizeOperationContract() returned an error: %v", err)
	}
	if normalized.Transport.WebSocket == original.WebSocket {
		t.Fatal("normalized WebSocket contract aliases the authoring value")
	}
	original.WebSocket.Socket.ClientToServerType = reflect.TypeFor[webSocketClientFixture]()
	if got := normalized.Transport.WebSocket.Socket.ClientToServerType; got != reflect.TypeFor[*typepb.Type]() {
		t.Fatalf("normalized client message type changed through alias: %v", got)
	}
}

func TestNormalizeOperationContractCompilesDirectionalSocketMessageDescriptors(t *testing.T) {
	transport := WebSocket(Socket[*typepb.Type, *typepb.Enum]().WithMessages(
		ClientMessage("  Message accepted from a client.  ", protobufTypeMessageContract{
			Name: MessageValue[string](Required(), MinLength(1)).Document(
				"Client type name.",
				Example("fenix.chat.ClientMessage"),
			),
		}),
		ServerMessage("Message delivered by the server.", protobufEnumMessageContract{
			Name: MessageValue[string](Required(), WithFormat("protobuf-full-name")).Document(
				"Server enum name.",
			),
		}),
	))
	normalized, err := normalizeOperationContract(OperationContract{Transport: transport})
	if err != nil {
		t.Fatalf("normalizeOperationContract() returned an error: %v", err)
	}
	socket := normalized.Transport.WebSocket.Socket
	if socket.ClientToServer == nil || socket.ClientToServer.Descriptor != nil ||
		socket.ClientToServer.Description != "Message accepted from a client." ||
		len(socket.ClientToServer.Bindings) != 1 {
		t.Fatalf("normalized client message = %#v", socket.ClientToServer)
	}
	clientName := socket.ClientToServer.Bindings[0]
	if !reflect.DeepEqual(clientName.FieldPath, []string{"Name"}) || !clientName.Input.Required ||
		clientName.Input.MinLength == nil || clientName.Documentation == nil ||
		clientName.Documentation.Example != "fenix.chat.ClientMessage" {
		t.Fatalf("normalized client name = %#v", clientName)
	}
	if socket.ServerToClient == nil || socket.ServerToClient.Descriptor != nil ||
		len(socket.ServerToClient.Bindings) != 1 ||
		socket.ServerToClient.Bindings[0].Input.Format != "protobuf-full-name" {
		t.Fatalf("normalized server message = %#v", socket.ServerToClient)
	}

	transport.WebSocket.Socket.ClientToServer.Description = "mutated"
	if socket.ClientToServer.Description != "Message accepted from a client." {
		t.Fatal("normalized directional metadata aliases its authoring value")
	}
}

func TestNormalizeOperationContractRejectsInvalidWebSocketMetadata(t *testing.T) {
	tests := []struct {
		name      string
		transport TransportContract
		want      string
	}{
		{
			name: "missing client message",
			transport: WebSocket(SocketContract{
				ServerToClientType: reflect.TypeFor[webSocketServerFixture](),
			}),
			want: "client-to-server message Go type is required",
		},
		{
			name: "anonymous JSON message",
			transport: WebSocket(SocketContract{
				ClientToServerType: reflect.TypeFor[struct{ Message string }](),
				ServerToClientType: reflect.TypeFor[webSocketServerFixture](),
			}),
			want: "must be a named struct",
		},
		{
			name: "missing server message",
			transport: WebSocket(SocketContract{
				ClientToServerType: reflect.TypeFor[webSocketClientFixture](),
			}),
			want: "server-to-client message Go type is required",
		},
		{
			name:      "mixed representations",
			transport: WebSocket(Socket[*typepb.Type, webSocketServerFixture]()),
			want:      "cannot mix protobuf and JSON",
		},
		{
			name: "blank directional description",
			transport: WebSocket(Socket[*typepb.Type, *anypb.Any]().WithMessages(
				ClientMessage(" ", protobufTypeMessageContract{}),
			)),
			want: "client-to-server message description is required",
		},
		{
			name: "directional field mismatch",
			transport: WebSocket(Socket[*typepb.Type, *anypb.Any]().WithMessages(
				ClientMessage("Client message.", struct {
					Name MessageValueBinding[int]
				}{Name: MessageValue[int]()}),
			)),
			want: "describes int, but response field has type string",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeOperationContract(OperationContract{Transport: test.transport})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeOperationContract() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateAPIDescriptionEnforcesWebSocketOperationSemantics(t *testing.T) {
	valid := func() OperationDescription {
		return OperationDescription{
			MethodName:  "Connect",
			OperationID: "fixture_connect",
			Route:       "/fixture/connect",
			HTTPMethod:  http.MethodGet,
			Contract: OperationContract{Transport: WebSocket(Socket[
				*typepb.Type,
				*anypb.Any,
			]())},
		}
	}
	if operation := valid(); operation.ResolvedSuccessStatus() != http.StatusSwitchingProtocols {
		t.Fatalf("WebSocket success status = %d, want 101", operation.ResolvedSuccessStatus())
	}
	if err := ValidateAPIDescription(APIDescription{Operations: []OperationDescription{valid()}}); err != nil {
		t.Fatalf("ValidateAPIDescription(valid WebSocket) returned an error: %v", err)
	}

	tests := []struct {
		name   string
		change func(*OperationDescription)
		want   string
	}{
		{name: "non GET", change: func(operation *OperationDescription) { operation.HTTPMethod = http.MethodPost }, want: "requires GET"},
		{name: "success override", change: func(operation *OperationDescription) { operation.Documentation.SuccessStatus = http.StatusOK }, want: "cannot override SuccessStatus"},
		{name: "response value", change: func(operation *OperationDescription) {
			operation.OutputType = reflect.TypeFor[webSocketServerFixture]()
		}, want: "cannot return a response value"},
		{name: "implicit body", change: func(operation *OperationDescription) { operation.InputType = reflect.TypeFor[webSocketClientFixture]() }, want: "cannot bind an HTTP request body"},
		{name: "explicit body binding", change: func(operation *OperationDescription) {
			operation.InputType = reflect.TypeFor[webSocketClientFixture]()
			operation.RequestBindings = []RequestBinding{{
				Source: BindingSourceBody,
				Input:  InputOf[webSocketClientFixture](Required()),
			}}
		}, want: "cannot bind an HTTP request body"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := valid()
			test.change(&operation)
			err := ValidateAPIDescription(APIDescription{Operations: []OperationDescription{operation}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateAPIDescription() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
