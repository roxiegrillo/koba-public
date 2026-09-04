package http

import (
	"errors"
	"io"
	stdhttp "net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestClientMiddlewareRunsRequestsForwardAndResponsesInReverse(t *testing.T) {
	var calls []string
	middleware := []Middleware{
		&tracingMiddleware{name: "first", calls: &calls},
		&tracingMiddleware{name: "second", calls: &calls},
	}
	transport := &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
		calls = append(calls, "transport")
		return &stdhttp.Response{StatusCode: stdhttp.StatusOK}, nil
	}}
	roundTripper := RequestClientMiddleware{Middleware: middleware, RoundTripper: transport}

	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := roundTripper.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() returned an error: %v", err)
	}
	if response.StatusCode != stdhttp.StatusOK {
		t.Fatalf("response status = %d, want %d", response.StatusCode, stdhttp.StatusOK)
	}
	want := []string{"request:first", "request:second", "transport", "response:second", "response:first"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("middleware calls = %#v, want %#v", calls, want)
	}
}

func TestRequestClientMiddlewareStopsWhenRequestMiddlewareFails(t *testing.T) {
	wantErr := errors.New("request rejected")
	transportCalled := false
	roundTripper := RequestClientMiddleware{
		Middleware: []Middleware{
			Request(func(*stdhttp.Request) error { return wantErr }),
		},
		RoundTripper: &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
			transportCalled = true
			return &stdhttp.Response{StatusCode: stdhttp.StatusOK}, nil
		}},
	}
	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := roundTripper.RoundTrip(request)
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
	if response != nil {
		t.Fatalf("RoundTrip() response = %#v, want nil", response)
	}
	if transportCalled {
		t.Fatal("transport ran after request middleware returned an error")
	}
}

func TestRequestClientMiddlewareUsesDefaultTransportWhenUnset(t *testing.T) {
	originalTransport := stdhttp.DefaultTransport
	stdhttp.DefaultTransport = &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
		return &stdhttp.Response{StatusCode: stdhttp.StatusNoContent, Body: stdhttp.NoBody}, nil
	}}
	t.Cleanup(func() { stdhttp.DefaultTransport = originalTransport })
	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := (&RequestClientMiddleware{}).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() returned an error: %v", err)
	}
	if got, want := response.StatusCode, stdhttp.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestRequestClientMiddlewareReturnsTransportErrorWithoutResponsePhase(t *testing.T) {
	wantErr := errors.New("dial failed")
	responseCalled := false
	roundTripper := RequestClientMiddleware{
		Middleware: []Middleware{
			Response(func(*stdhttp.Response) error {
				responseCalled = true
				return nil
			}),
		},
		RoundTripper: &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
			return nil, wantErr
		}},
	}
	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := roundTripper.RoundTrip(request)
	if !errors.Is(err, wantErr) || response != nil {
		t.Fatalf("RoundTrip() = (%#v, %v), want (nil, %v)", response, err, wantErr)
	}
	if responseCalled {
		t.Fatal("response middleware ran after the transport failed")
	}
}

func TestRequestClientMiddlewareClosesBodyWhenResponseMiddlewareFails(t *testing.T) {
	wantErr := errors.New("invalid response")
	body := &observedResponseBody{Reader: strings.NewReader("payload")}
	roundTripper := RequestClientMiddleware{
		Middleware: []Middleware{
			Response(func(*stdhttp.Response) error { return wantErr }),
		},
		RoundTripper: &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
			return &stdhttp.Response{StatusCode: stdhttp.StatusOK, Body: body}, nil
		}},
	}
	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := roundTripper.RoundTrip(request)
	if !errors.Is(err, wantErr) || response != nil {
		t.Fatalf("RoundTrip() = (%#v, %v), want (nil, %v)", response, err, wantErr)
	}
	if !body.closed {
		t.Fatal("response body remained open after response middleware failed")
	}
}

func TestRequestAndResponseAdaptersOnlyRunTheirConfiguredPhase(t *testing.T) {
	requestCalls := 0
	responseCalls := 0
	requestMiddleware := Request(func(*stdhttp.Request) error {
		requestCalls++
		return nil
	})
	responseMiddleware := Response(func(*stdhttp.Response) error {
		responseCalls++
		return nil
	})
	request, err := stdhttp.NewRequest(stdhttp.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &stdhttp.Response{StatusCode: stdhttp.StatusOK}

	if err := requestMiddleware.Request(request); err != nil {
		t.Fatal(err)
	}
	if err := requestMiddleware.Response(response); err != nil {
		t.Fatal(err)
	}
	if err := responseMiddleware.Request(request); err != nil {
		t.Fatal(err)
	}
	if err := responseMiddleware.Response(response); err != nil {
		t.Fatal(err)
	}
	if requestCalls != 1 || responseCalls != 1 {
		t.Fatalf("adapter calls = request:%d response:%d, want request:1 response:1", requestCalls, responseCalls)
	}
}

type tracingMiddleware struct {
	name  string
	calls *[]string
}

type observedResponseBody struct {
	io.Reader
	closed bool
}

func (body *observedResponseBody) Close() error {
	body.closed = true
	return nil
}

func (m *tracingMiddleware) Request(*stdhttp.Request) error {
	*m.calls = append(*m.calls, "request:"+m.name)
	return nil
}

func (m *tracingMiddleware) Response(*stdhttp.Response) error {
	*m.calls = append(*m.calls, "response:"+m.name)
	return nil
}
