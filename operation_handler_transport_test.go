package koba

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
)

type countingHTTPResponseWriter struct {
	httprouter.ResponseWriter
	writeHeaders []int
}

func (writer *countingHTTPResponseWriter) WriteHeader(status int) {
	writer.writeHeaders = append(writer.writeHeaders, status)
	writer.ResponseWriter.WriteHeader(status)
}

func TestOperationHandlerRequiresNativeWebSocketAcceptance(t *testing.T) {
	newContext := func() (*httprouter.Context, *countingHTTPResponseWriter) {
		context, _ := httprouter.CreateTestContext(httptest.NewRecorder())
		writer := &countingHTTPResponseWriter{ResponseWriter: context.Writer}
		context.Writer = writer
		context.Request = httptest.NewRequest(http.MethodGet, "/connect", nil)
		return context, writer
	}
	webSocketContext, webSocketWriter := newContext()
	invocations := 0
	makeOperationHandler(OperationDescription{
		MethodName: "Connect",
		HTTPMethod: http.MethodGet,
		Contract: OperationContract{Transport: WebSocket(Socket[
			webSocketClientFixture,
			webSocketServerFixture,
		]())},
	}, func(Invocation) (InvocationResult, error) {
		invocations++
		return InvocationResult{}, nil
	})(webSocketContext)
	if got, want := webSocketWriter.writeHeaders, []int{http.StatusInternalServerError}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("WebSocket handler status writes = %v, want missing-acceptance failure %v", got, want)
	}

	restContext, restWriter := newContext()
	makeOperationHandler(OperationDescription{
		MethodName: "Get",
		HTTPMethod: http.MethodGet,
	}, func(Invocation) (InvocationResult, error) {
		invocations++
		return InvocationResult{}, nil
	})(restContext)
	if got, want := restWriter.writeHeaders, []int{http.StatusNoContent}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ordinary handler status writes = %v, want %v", got, want)
	}
	if invocations != 2 {
		t.Fatalf("invocations = %d, want 2", invocations)
	}
}
