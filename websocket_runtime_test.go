package koba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	fberrors "github.com/roxiegrillo/koba/v2/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/typepb"
)

type nativeSocketAPI struct {
	normalClose chan struct{}
}

func (*nativeSocketAPI) JSON(ctx context.Context) {
	err := AcceptWebSocket[webSocketClientFixture, webSocketServerFixture](ctx, func(
		_ context.Context,
		socket SocketConnection[webSocketClientFixture, webSocketServerFixture],
	) error {
		request, err := socket.Receive()
		if err != nil {
			return err
		}
		return socket.Send(webSocketServerFixture{ID: "server", Message: request.Message})
	})
	if err != nil {
		panic(err)
	}
}

func (*nativeSocketAPI) Protobuf(ctx context.Context) {
	err := AcceptWebSocket[*typepb.Type, *anypb.Any](ctx, func(
		_ context.Context,
		socket SocketConnection[*typepb.Type, *anypb.Any],
	) error {
		request, err := socket.Receive()
		if err != nil {
			return err
		}
		return socket.Send(&anypb.Any{TypeUrl: request.GetName()})
	})
	if err != nil {
		panic(err)
	}
}

func (*nativeSocketAPI) Reject(context.Context) {
	panic(fberrors.MakeError(http.StatusConflict, "socket is already active"))
}

func (*nativeSocketAPI) ReturnReject(context.Context) error {
	return fberrors.MakeError(http.StatusConflict, "socket is already active")
}

func (*nativeSocketAPI) ReturnFailure(context.Context) error {
	return errors.New("socket preparation failed")
}

func (*nativeSocketAPI) ReturnAfterUpgrade(ctx context.Context) error {
	return AcceptWebSocket[webSocketClientFixture, webSocketServerFixture](ctx, func(
		context.Context,
		SocketConnection[webSocketClientFixture, webSocketServerFixture],
	) error {
		return errors.New("socket callback failed")
	})
}

func (*nativeSocketAPI) Mismatch(ctx context.Context) {
	if err := AcceptWebSocket[webSocketServerFixture, webSocketClientFixture](ctx, func(
		context.Context,
		SocketConnection[webSocketServerFixture, webSocketClientFixture],
	) error {
		return nil
	}); err != nil {
		panic(err)
	}
}

func (*nativeSocketAPI) PanicAfterUpgrade(ctx context.Context) {
	if err := AcceptWebSocket[webSocketClientFixture, webSocketServerFixture](ctx, func(
		context.Context,
		SocketConnection[webSocketClientFixture, webSocketServerFixture],
	) error {
		panic("socket callback panic")
	}); err != nil {
		panic(err)
	}
}

func (api *nativeSocketAPI) PeerClose(ctx context.Context) {
	err := AcceptWebSocket[webSocketClientFixture, webSocketServerFixture](ctx, func(
		_ context.Context,
		socket SocketConnection[webSocketClientFixture, webSocketServerFixture],
	) error {
		_, err := socket.Receive()
		return err
	})
	if err != nil {
		panic(err)
	}
	if api.normalClose != nil {
		close(api.normalClose)
	}
}

func (*nativeSocketAPI) Routes() map[string]string {
	return map[string]string{
		"JSON":               "/socket/json",
		"Protobuf":           "/socket/protobuf",
		"Reject":             "/socket/reject",
		"ReturnReject":       "/socket/return-reject",
		"ReturnFailure":      "/socket/return-failure",
		"ReturnAfterUpgrade": "/socket/return-after-upgrade",
		"Mismatch":           "/socket/mismatch",
		"PanicAfterUpgrade":  "/socket/panic",
		"PeerClose":          "/socket/peer-close",
	}
}

func (*nativeSocketAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"JSON":               {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"Protobuf":           {Transport: WebSocket(Socket[*typepb.Type, *anypb.Any]())},
		"Reject":             {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"ReturnReject":       {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"ReturnFailure":      {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"ReturnAfterUpgrade": {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"Mismatch":           {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"PanicAfterUpgrade":  {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
		"PeerClose":          {Transport: WebSocket(Socket[webSocketClientFixture, webSocketServerFixture]())},
	}}
}

func TestNativeWebSocketRuntimeInfersJSONAndProtobufCodecs(t *testing.T) {
	normalClose := make(chan struct{})
	app := MakeRawApp()
	app.AddApi(&nativeSocketAPI{normalClose: normalClose})
	server := httptest.NewServer(app.(http.Handler))
	defer server.Close()

	t.Run("JSON text frames", func(t *testing.T) {
		connection, _, err := websocket.DefaultDialer.Dial(webSocketTestURL(server.URL, "/socket/json"), nil)
		if err != nil {
			t.Fatalf("dial JSON WebSocket: %v", err)
		}
		defer connection.Close()
		if err := connection.WriteJSON(webSocketClientFixture{Message: "hello"}); err != nil {
			t.Fatalf("write JSON message: %v", err)
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read JSON response: %v", err)
		}
		if messageType != websocket.TextMessage {
			t.Fatalf("JSON response frame type = %d, want text", messageType)
		}
		var response webSocketServerFixture
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatalf("decode JSON response: %v", err)
		}
		if response.ID != "server" || response.Message != "hello" {
			t.Fatalf("JSON response = %#v", response)
		}
	})

	t.Run("protobuf binary frames", func(t *testing.T) {
		connection, _, err := websocket.DefaultDialer.Dial(webSocketTestURL(server.URL, "/socket/protobuf"), nil)
		if err != nil {
			t.Fatalf("dial protobuf WebSocket: %v", err)
		}
		defer connection.Close()
		payload, err := proto.Marshal(&typepb.Type{Name: "fixture.Message"})
		if err != nil {
			t.Fatalf("marshal protobuf request: %v", err)
		}
		if err := connection.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			t.Fatalf("write protobuf message: %v", err)
		}
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read protobuf response: %v", err)
		}
		if messageType != websocket.BinaryMessage {
			t.Fatalf("protobuf response frame type = %d, want binary", messageType)
		}
		response := &anypb.Any{}
		if err := proto.Unmarshal(payload, response); err != nil {
			t.Fatalf("decode protobuf response: %v", err)
		}
		if response.GetTypeUrl() != "fixture.Message" {
			t.Fatalf("protobuf response = %#v", response)
		}
	})

	for _, test := range []struct {
		name        string
		path        string
		messageType int
		payload     []byte
		closeCode   int
	}{
		{
			name:        "protobuf rejects text frames",
			path:        "/socket/protobuf",
			messageType: websocket.TextMessage,
			payload:     []byte(`{"name":"wrong framing"}`),
			closeCode:   WebSocketCloseUnsupportedData,
		},
		{
			name:        "protobuf rejects malformed payload",
			path:        "/socket/protobuf",
			messageType: websocket.BinaryMessage,
			payload:     []byte{0xff, 0xff, 0xff},
			closeCode:   WebSocketCloseInvalidFramePayloadData,
		},
		{
			name:        "JSON rejects binary frames",
			path:        "/socket/json",
			messageType: websocket.BinaryMessage,
			payload:     []byte(`{"message":"wrong framing"}`),
			closeCode:   WebSocketCloseUnsupportedData,
		},
		{
			name:        "JSON rejects malformed payload",
			path:        "/socket/json",
			messageType: websocket.TextMessage,
			payload:     []byte(`{"message":`),
			closeCode:   WebSocketCloseInvalidFramePayloadData,
		},
		{
			name:        "read limit closes an oversized message",
			path:        "/socket/protobuf",
			messageType: websocket.BinaryMessage,
			payload:     make([]byte, defaultWebSocketReadLimit+1),
			closeCode:   WebSocketCloseMessageTooBig,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, _, err := websocket.DefaultDialer.Dial(webSocketTestURL(server.URL, test.path), nil)
			if err != nil {
				t.Fatalf("dial WebSocket: %v", err)
			}
			defer connection.Close()
			if err := connection.WriteMessage(test.messageType, test.payload); err != nil {
				t.Fatalf("write invalid protobuf frame: %v", err)
			}
			if _, _, err := connection.ReadMessage(); err == nil {
				t.Fatal("invalid protobuf frame did not close the connection")
			} else if !websocket.IsCloseError(err, test.closeCode) {
				t.Fatalf("invalid frame close error = %v, want status %d", err, test.closeCode)
			}
		})
	}

	t.Run("panic after upgrade closes without an HTTP epilogue", func(t *testing.T) {
		connection, response, err := websocket.DefaultDialer.Dial(webSocketTestURL(server.URL, "/socket/panic"), nil)
		if err != nil {
			t.Fatalf("dial panic WebSocket: %v (response %#v)", err, response)
		}
		defer connection.Close()
		if response.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("upgrade status = %d, want 101", response.StatusCode)
		}
		if _, _, err := connection.ReadMessage(); err == nil {
			t.Fatal("callback panic produced a post-upgrade data frame")
		} else if !websocket.IsCloseError(err, WebSocketCloseInternalError) {
			t.Fatalf("callback panic close error = %v, want status %d", err, WebSocketCloseInternalError)
		}
	})

	t.Run("normal peer close remains a normal callback result", func(t *testing.T) {
		connection, _, err := websocket.DefaultDialer.Dial(webSocketTestURL(server.URL, "/socket/peer-close"), nil)
		if err != nil {
			t.Fatalf("dial peer-close WebSocket: %v", err)
		}
		defer connection.Close()
		if err := connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		); err != nil {
			t.Fatalf("write normal peer close: %v", err)
		}
		select {
		case <-normalClose:
		case <-time.After(time.Second):
			t.Fatal("normal peer close was not returned as a successful acceptance")
		}
	})
}

type closingGateWebSocketWire struct {
	controlWritten chan struct{}
	allowClose     chan struct{}
	dataWritten    chan struct{}
	mutex          sync.Mutex
	events         []string
}

func (*closingGateWebSocketWire) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("not implemented")
}

func (*closingGateWebSocketWire) SetWriteDeadline(time.Time) error { return nil }

func (wire *closingGateWebSocketWire) WriteMessage(int, []byte) error {
	wire.record("data")
	close(wire.dataWritten)
	return nil
}

func (wire *closingGateWebSocketWire) WriteControl(int, []byte, time.Time) error {
	wire.record("control")
	close(wire.controlWritten)
	return nil
}

func (wire *closingGateWebSocketWire) Close() error {
	<-wire.allowClose
	wire.record("close")
	return nil
}

func (wire *closingGateWebSocketWire) record(event string) {
	wire.mutex.Lock()
	defer wire.mutex.Unlock()
	wire.events = append(wire.events, event)
}

func (wire *closingGateWebSocketWire) snapshot() []string {
	wire.mutex.Lock()
	defer wire.mutex.Unlock()
	return append([]string(nil), wire.events...)
}

func TestTypedSocketCloseSerializesWithSendAndRunsOnce(t *testing.T) {
	wire := &closingGateWebSocketWire{
		controlWritten: make(chan struct{}),
		allowClose:     make(chan struct{}),
		dataWritten:    make(chan struct{}),
	}
	socket := &typedSocketConnection[webSocketClientFixture, webSocketServerFixture]{
		connection: wire,
		frameType:  WebSocketFrameText,
		encoding:   WebSocketEncodingJSON,
	}
	closeResult := make(chan error, 1)
	go func() {
		closeResult <- socket.CloseWithStatus(WebSocketCloseNormalClosure, "done")
	}()
	<-wire.controlWritten

	sendResult := make(chan error, 1)
	go func() {
		sendResult <- socket.Send(webSocketServerFixture{ID: "late"})
	}()
	select {
	case <-wire.dataWritten:
		t.Fatal("Send wrote a data frame after the close control frame")
	case <-time.After(25 * time.Millisecond):
	}
	close(wire.allowClose)
	if err := <-closeResult; err != nil {
		t.Fatalf("CloseWithStatus() error = %v", err)
	}
	if err := <-sendResult; err == nil || !strings.Contains(err.Error(), "connection is closed") {
		t.Fatalf("Send() after close error = %v, want closed connection", err)
	}
	if err := socket.CloseWithStatus(WebSocketCloseInternalError, "again"); err != nil {
		t.Fatalf("second CloseWithStatus() error = %v", err)
	}
	if got, want := wire.snapshot(), []string{"control", "close"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wire events = %v, want %v", got, want)
	}
}

func TestNativeWebSocketRuntimePreservesHTTPPreparationBoundary(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&nativeSocketAPI{})
	server := httptest.NewServer(app.(http.Handler))
	defer server.Close()

	response, err := http.Get(server.URL + "/socket/reject")
	if err != nil {
		t.Fatalf("GET rejected socket: %v", err)
	}
	defer response.Body.Close()
	if got, want := response.StatusCode, http.StatusConflict; got != want {
		t.Fatalf("rejected handshake status = %d, want %d", got, want)
	}

	mismatch, err := http.Get(server.URL + "/socket/mismatch")
	if err != nil {
		t.Fatalf("GET mismatched socket: %v", err)
	}
	defer mismatch.Body.Close()
	if got, want := mismatch.StatusCode, http.StatusInternalServerError; got != want {
		t.Fatalf("mismatched acceptance status = %d, want %d", got, want)
	}
}

func TestNativeWebSocketRuntimeMapsReturnedErrorsBeforeAcceptance(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&nativeSocketAPI{})
	server := httptest.NewServer(app.(http.Handler))
	defer server.Close()

	for _, test := range []struct {
		name        string
		path        string
		wantStatus  int
		wantError   string
		wantMessage string
	}{
		{
			name:        "status-coded error",
			path:        "/socket/return-reject",
			wantStatus:  http.StatusConflict,
			wantError:   "conflict",
			wantMessage: "socket is already active",
		},
		{
			name:        "generic error",
			path:        "/socket/return-failure",
			wantStatus:  http.StatusInternalServerError,
			wantError:   "internal_server_error",
			wantMessage: "socket preparation failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := http.Get(server.URL + test.path)
			if err != nil {
				t.Fatalf("GET returned WebSocket error: %v", err)
			}
			defer response.Body.Close()
			if got := response.StatusCode; got != test.wantStatus {
				t.Fatalf("status = %d, want %d", got, test.wantStatus)
			}

			var body struct {
				Status  int    `json:"status"`
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Status != test.wantStatus || body.Error != test.wantError || body.Message != test.wantMessage {
				t.Fatalf("error response = %#v, want status=%d error=%q message=%q", body, test.wantStatus, test.wantError, test.wantMessage)
			}
		})
	}
}

func TestNativeWebSocketRuntimeReturnsAcceptanceErrorWithoutHTTPEpilogue(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&nativeSocketAPI{})
	server := httptest.NewServer(app.(http.Handler))
	defer server.Close()

	connection, response, err := websocket.DefaultDialer.Dial(
		webSocketTestURL(server.URL, "/socket/return-after-upgrade"),
		nil,
	)
	if err != nil {
		t.Fatalf("dial returned-error WebSocket: %v (response %#v)", err, response)
	}
	defer connection.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", response.StatusCode)
	}
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("returned callback error produced a post-upgrade data frame")
	} else if !websocket.IsCloseError(err, WebSocketCloseInternalError) {
		t.Fatalf("returned callback close error = %v, want status %d", err, WebSocketCloseInternalError)
	}
}

func TestAcceptWebSocketRequiresOperationContext(t *testing.T) {
	err := AcceptWebSocket[webSocketClientFixture, webSocketServerFixture](context.Background(), func(
		context.Context,
		SocketConnection[webSocketClientFixture, webSocketServerFixture],
	) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "must be called from a WebSocket operation") {
		t.Fatalf("AcceptWebSocket() error = %v", err)
	}
}

func webSocketTestURL(serverURL, path string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http") + path
}
