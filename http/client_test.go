package http

import (
	stdhttp "net/http"
	"testing"

	client_middleware "github.com/roxiegrillo/koba/v2/http/client-middleware"
)

type stubRoundTripper struct {
	roundTrip func(*stdhttp.Request) (*stdhttp.Response, error)
}

func (s *stubRoundTripper) RoundTrip(request *stdhttp.Request) (*stdhttp.Response, error) {
	return s.roundTrip(request)
}

func TestMakeDefaultKobaClientBuildsMiddlewareChain(t *testing.T) {
	transport := &stubRoundTripper{roundTrip: func(*stdhttp.Request) (*stdhttp.Response, error) {
		return &stdhttp.Response{StatusCode: stdhttp.StatusNoContent}, nil
	}}
	calls := []string{}
	customMiddleware := &tracingMiddleware{name: "custom", calls: &calls}
	builder := KobaClientBuilder{
		Middleware: []Middleware{customMiddleware},
		Transport:  transport,
	}

	client := builder.MakeDefaultKobaClient()
	if client == nil {
		t.Fatal("MakeDefaultKobaClient() returned nil")
	}
	roundTripper, ok := client.Transport.(*RequestClientMiddleware)
	if !ok {
		t.Fatalf("client.Transport = %T, want *RequestClientMiddleware", client.Transport)
	}
	if roundTripper.RoundTripper != transport {
		t.Fatalf("wrapped transport = %T, want the explicitly configured transport", roundTripper.RoundTripper)
	}
	if got, want := len(roundTripper.Middleware), 2; got != want {
		t.Fatalf("middleware count = %d, want %d", got, want)
	}
	if _, ok := roundTripper.Middleware[0].(*client_middleware.AddHeader); !ok {
		t.Fatalf("first middleware = %T, want *client_middleware.AddHeader", roundTripper.Middleware[0])
	}
	if roundTripper.Middleware[1] != customMiddleware {
		t.Fatal("custom middleware was not appended after the default middleware")
	}
}

func TestMakeDefaultKobaClientUsesDefaultTransportWhenUnset(t *testing.T) {
	client := (&KobaClientBuilder{}).MakeDefaultKobaClient()
	roundTripper, ok := client.Transport.(*RequestClientMiddleware)
	if !ok {
		t.Fatalf("client.Transport = %T, want *RequestClientMiddleware", client.Transport)
	}
	if roundTripper.RoundTripper != stdhttp.DefaultTransport {
		t.Fatalf("wrapped transport = %T, want http.DefaultTransport", roundTripper.RoundTripper)
	}
}
