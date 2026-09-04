package client_middleware

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	http_common "github.com/roxiegrillo/koba/v2/http/common"
)

func TestAddHeaderCopiesOnlySelectedContextHeaders(t *testing.T) {
	contextHeaders := http.Header{
		"X-Request-Id": []string{"request-123"},
		"X-Internal":   []string{"do-not-forward"},
	}
	request, err := http.NewRequest(http.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(context.WithValue(request.Context(), http_common.Headers, contextHeaders))
	middleware := AddHeader{Headers: []string{"X-Request-Id"}}

	if err := middleware.Request(request); err != nil {
		t.Fatalf("Request() returned an error: %v", err)
	}
	if got, want := request.Header.Get("X-Request-Id"), "request-123"; got != want {
		t.Fatalf("X-Request-Id = %q, want %q", got, want)
	}
	if got := request.Header.Get("X-Internal"); got != "" {
		t.Fatalf("unselected X-Internal header was forwarded: %q", got)
	}
}

func TestAddHeaderCopiesMultipleValues(t *testing.T) {
	contextHeaders := http.Header{
		"X-Scope": []string{"items:read", "items:write"},
	}
	request, err := http.NewRequest(http.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(context.WithValue(request.Context(), http_common.Headers, contextHeaders))
	middleware := AddHeader{Headers: []string{"X-Scope"}}

	if err := middleware.Request(request); err != nil {
		t.Fatalf("Request() returned an error: %v", err)
	}
	if got, want := request.Header.Values("X-Scope"), []string{"items:read", "items:write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X-Scope values = %#v, want %#v", got, want)
	}
}

func TestAddHeaderResponseIsNoop(t *testing.T) {
	if err := (&AddHeader{}).Response(&http.Response{}); err != nil {
		t.Fatalf("Response() returned an error: %v", err)
	}
}
