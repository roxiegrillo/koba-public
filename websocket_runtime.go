package koba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const (
	defaultWebSocketReadLimit        = 1 << 20
	defaultWebSocketHandshakeTimeout = 10 * time.Second
	defaultWebSocketWriteTimeout     = 10 * time.Second

	// WebSocketCloseNormalClosure is the RFC 6455 normal closure status.
	WebSocketCloseNormalClosure = websocket.CloseNormalClosure
	// WebSocketCloseGoingAway indicates that an endpoint is going away.
	WebSocketCloseGoingAway = websocket.CloseGoingAway
	// WebSocketCloseUnsupportedData indicates an unexpected frame representation.
	WebSocketCloseUnsupportedData = websocket.CloseUnsupportedData
	// WebSocketCloseInvalidFramePayloadData indicates a payload that cannot be decoded.
	WebSocketCloseInvalidFramePayloadData = websocket.CloseInvalidFramePayloadData
	// WebSocketCloseMessageTooBig indicates a frame above the configured read limit.
	WebSocketCloseMessageTooBig = websocket.CloseMessageTooBig
	// WebSocketClosePolicyViolation indicates a message rejected by application policy.
	WebSocketClosePolicyViolation = websocket.ClosePolicyViolation
	// WebSocketCloseInternalError indicates an unexpected server failure.
	WebSocketCloseInternalError = websocket.CloseInternalServerErr
)

// SocketConnection is the typed application-facing side of an accepted
// WebSocket. Receive decodes client-to-server frames and Send encodes
// server-to-client frames using the representation inferred by Socket.
// Implementations are safe for one concurrent reader and multiple writers.
type SocketConnection[ClientToServer any, ServerToClient any] interface {
	Receive() (ClientToServer, error)
	Send(ServerToClient) error
	Close() error
	CloseWithStatus(code int, reason string) error
}

// WebSocketCloseError reports a close control frame without exposing the
// framework's WebSocket implementation to application code.
type WebSocketCloseError struct {
	Code   int
	Reason string
}

func (err *WebSocketCloseError) Error() string {
	return fmt.Sprintf("WebSocket closed with status %d: %s", err.Code, err.Reason)
}

// IsWebSocketCloseError reports whether err is a peer close with one of the
// supplied status codes. With no codes it matches every peer close.
func IsWebSocketCloseError(err error, codes ...int) bool {
	var closeError *WebSocketCloseError
	if !errors.As(err, &closeError) {
		return false
	}
	if len(codes) == 0 {
		return true
	}
	for _, code := range codes {
		if closeError.Code == code {
			return true
		}
	}
	return false
}

type webSocketAcceptanceContextKey struct{}

type webSocketAcceptance struct {
	mutex     sync.Mutex
	writer    http.ResponseWriter
	request   *http.Request
	contract  WebSocketTransportContract
	attempted bool
	upgraded  bool
}

func contextWithWebSocketAcceptance(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	contract WebSocketTransportContract,
) (context.Context, *webSocketAcceptance) {
	acceptance := &webSocketAcceptance{writer: writer, request: request, contract: contract}
	return context.WithValue(ctx, webSocketAcceptanceContextKey{}, acceptance), acceptance
}

func (acceptance *webSocketAcceptance) state() (attempted, upgraded bool) {
	acceptance.mutex.Lock()
	defer acceptance.mutex.Unlock()
	return acceptance.attempted, acceptance.upgraded
}

// AcceptWebSocket upgrades the current operation's validated HTTP request and
// runs handler with a typed socket. Call it only from an operation declared
// with WebSocket(Socket[ClientToServer, ServerToClient]()). Domain validation
// performed before this call can still return ordinary HTTP errors.
func AcceptWebSocket[ClientToServer any, ServerToClient any](
	ctx context.Context,
	handler func(context.Context, SocketConnection[ClientToServer, ServerToClient]) error,
) error {
	acceptance, ok := ctx.Value(webSocketAcceptanceContextKey{}).(*webSocketAcceptance)
	if !ok || acceptance == nil {
		return fmt.Errorf("koba: AcceptWebSocket must be called from a WebSocket operation")
	}
	if handler == nil {
		return fmt.Errorf("koba: AcceptWebSocket handler is required")
	}
	clientType := reflect.TypeFor[ClientToServer]()
	serverType := reflect.TypeFor[ServerToClient]()
	if clientType != acceptance.contract.Socket.ClientToServerType || serverType != acceptance.contract.Socket.ServerToClientType {
		return fmt.Errorf(
			"koba: AcceptWebSocket types [%s, %s] do not match the operation socket [%s, %s]",
			clientType,
			serverType,
			acceptance.contract.Socket.ClientToServerType,
			acceptance.contract.Socket.ServerToClientType,
		)
	}

	acceptance.mutex.Lock()
	if acceptance.attempted {
		acceptance.mutex.Unlock()
		return fmt.Errorf("koba: WebSocket operation has already attempted an upgrade")
	}
	acceptance.attempted = true
	acceptance.mutex.Unlock()

	connection, err := (&websocket.Upgrader{
		HandshakeTimeout: defaultWebSocketHandshakeTimeout,
	}).Upgrade(acceptance.writer, acceptance.request, nil)
	if err != nil {
		return fmt.Errorf("koba: upgrade WebSocket: %w", err)
	}
	acceptance.mutex.Lock()
	acceptance.upgraded = true
	acceptance.mutex.Unlock()
	connection.SetReadLimit(defaultWebSocketReadLimit)

	socket := &typedSocketConnection[ClientToServer, ServerToClient]{
		connection: connection,
		frameType:  acceptance.contract.ResolvedFrameType(),
		encoding:   acceptance.contract.ResolvedEncoding(),
	}
	defer socket.Close()
	stopCloseOnCancellation := context.AfterFunc(ctx, func() { _ = socket.Close() })
	defer stopCloseOnCancellation()
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = socket.CloseWithStatus(WebSocketCloseInternalError, "internal server error")
			panic(recovered)
		}
	}()
	handlerError := handler(ctx, socket)
	if handlerError != nil {
		if IsWebSocketCloseError(
			handlerError,
			WebSocketCloseNormalClosure,
			WebSocketCloseGoingAway,
		) {
			_ = socket.Close()
			return nil
		}
		if IsWebSocketCloseError(handlerError) {
			_ = socket.Close()
			return handlerError
		}
		var protocolError *webSocketProtocolError
		if errors.As(handlerError, &protocolError) {
			_ = socket.Close()
			return handlerError
		}
		_ = socket.CloseWithStatus(WebSocketCloseInternalError, "internal server error")
		return handlerError
	}
	_ = socket.CloseWithStatus(WebSocketCloseNormalClosure, "")
	return nil
}

type typedSocketConnection[ClientToServer any, ServerToClient any] struct {
	connection webSocketWireConnection
	frameType  WebSocketFrameType
	encoding   WebSocketEncoding
	writeMutex sync.Mutex
	closeOnce  sync.Once
	closing    atomic.Bool
	closeError error
}

type webSocketWireConnection interface {
	ReadMessage() (messageType int, payload []byte, err error)
	SetWriteDeadline(time.Time) error
	WriteMessage(messageType int, payload []byte) error
	WriteControl(messageType int, payload []byte, deadline time.Time) error
	Close() error
}

func (socket *typedSocketConnection[ClientToServer, ServerToClient]) Receive() (ClientToServer, error) {
	var zero ClientToServer
	messageType, payload, err := socket.connection.ReadMessage()
	if err != nil {
		if errors.Is(err, websocket.ErrReadLimit) {
			return zero, socket.protocolFailure(
				WebSocketCloseMessageTooBig,
				"message too big",
				err,
				true,
			)
		}
		return zero, normalizeWebSocketError(err)
	}
	wantMessageType := websocket.TextMessage
	if socket.frameType == WebSocketFrameBinary {
		wantMessageType = websocket.BinaryMessage
	}
	if messageType != wantMessageType {
		return zero, socket.protocolFailure(
			WebSocketCloseUnsupportedData,
			"unsupported data",
			fmt.Errorf(
				"koba: received WebSocket frame type %d, want %s",
				messageType,
				socket.frameType,
			),
			false,
		)
	}
	if socket.encoding == WebSocketEncodingProtobuf {
		value, message, err := newProtobufSocketMessage[ClientToServer]()
		if err != nil {
			return zero, err
		}
		if err := proto.Unmarshal(payload, message); err != nil {
			return zero, socket.protocolFailure(
				WebSocketCloseInvalidFramePayloadData,
				"invalid protobuf payload",
				fmt.Errorf("koba: decode protobuf WebSocket frame: %w", err),
				false,
			)
		}
		return value, nil
	}
	var value ClientToServer
	if err := json.Unmarshal(payload, &value); err != nil {
		return zero, socket.protocolFailure(
			WebSocketCloseInvalidFramePayloadData,
			"invalid JSON payload",
			fmt.Errorf("koba: decode JSON WebSocket frame: %w", err),
			false,
		)
	}
	return value, nil
}

type webSocketProtocolError struct {
	cause error
}

func (err *webSocketProtocolError) Error() string { return err.cause.Error() }

func (err *webSocketProtocolError) Unwrap() error { return err.cause }

func (socket *typedSocketConnection[ClientToServer, ServerToClient]) protocolFailure(
	code int,
	reason string,
	cause error,
	closeAlreadySent bool,
) error {
	if closeAlreadySent {
		_ = socket.Close()
	} else {
		_ = socket.CloseWithStatus(code, reason)
	}
	return &webSocketProtocolError{cause: cause}
}

func (socket *typedSocketConnection[ClientToServer, ServerToClient]) Send(value ServerToClient) error {
	var (
		payload     []byte
		messageType = websocket.TextMessage
		err         error
	)
	if socket.encoding == WebSocketEncodingProtobuf {
		message, ok := any(value).(proto.Message)
		if !ok || message == nil || (reflect.ValueOf(message).Kind() == reflect.Pointer && reflect.ValueOf(message).IsNil()) {
			return fmt.Errorf("koba: server-to-client value %T is not a non-nil protobuf message", value)
		}
		payload, err = proto.Marshal(message)
		messageType = websocket.BinaryMessage
	} else {
		payload, err = json.Marshal(value)
	}
	if err != nil {
		return fmt.Errorf("koba: encode %s WebSocket frame: %w", socket.encoding, err)
	}

	socket.writeMutex.Lock()
	defer socket.writeMutex.Unlock()
	if socket.closing.Load() {
		return fmt.Errorf("koba: WebSocket connection is closed")
	}
	if err := socket.connection.SetWriteDeadline(time.Now().Add(defaultWebSocketWriteTimeout)); err != nil {
		return fmt.Errorf("koba: set WebSocket write deadline: %w", err)
	}
	if err := socket.connection.WriteMessage(messageType, payload); err != nil {
		return normalizeWebSocketError(err)
	}
	return nil
}

func (socket *typedSocketConnection[ClientToServer, ServerToClient]) Close() error {
	socket.closing.Store(true)
	socket.closeOnce.Do(func() {
		socket.writeMutex.Lock()
		defer socket.writeMutex.Unlock()
		socket.closeError = socket.connection.Close()
	})
	return socket.closeError
}

func (socket *typedSocketConnection[ClientToServer, ServerToClient]) CloseWithStatus(code int, reason string) error {
	socket.closing.Store(true)
	socket.closeOnce.Do(func() {
		socket.writeMutex.Lock()
		defer socket.writeMutex.Unlock()
		writeError := socket.connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason),
			time.Now().Add(defaultWebSocketWriteTimeout),
		)
		closeError := socket.connection.Close()
		if writeError != nil {
			socket.closeError = normalizeWebSocketError(writeError)
			return
		}
		socket.closeError = closeError
	})
	return socket.closeError
}

func newProtobufSocketMessage[Message any]() (Message, proto.Message, error) {
	var zero Message
	messageType := reflect.TypeFor[Message]()
	if messageType == nil || messageType.Kind() != reflect.Pointer || messageType.Elem().Kind() != reflect.Struct {
		return zero, nil, fmt.Errorf("koba: protobuf socket message type must be a pointer to a struct, got %s", messageType)
	}
	value := reflect.New(messageType.Elem()).Interface()
	message, ok := value.(proto.Message)
	if !ok {
		return zero, nil, fmt.Errorf("koba: protobuf socket message type %s does not implement proto.Message", messageType)
	}
	typed, ok := value.(Message)
	if !ok {
		return zero, nil, fmt.Errorf("koba: cannot construct protobuf socket message type %s", messageType)
	}
	return typed, message, nil
}

func normalizeWebSocketError(err error) error {
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		return &WebSocketCloseError{Code: closeError.Code, Reason: closeError.Text}
	}
	return err
}
