package client_middleware

import (
	"net/http"
	"reflect"
	"testing"
)

func TestWithHeadersSetsSimpleHeaderAndHost(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	middleware := WithHeaders{Headers: http.Header{
		"X-Service": []string{"fenix-items-api"},
		"Host":      []string{"internal.example"},
	}}

	if err := middleware.Request(request); err != nil {
		t.Fatalf("Request() returned an error: %v", err)
	}
	if got, want := request.Header.Get("X-Service"), "fenix-items-api"; got != want {
		t.Fatalf("X-Service = %q, want %q", got, want)
	}
	if got, want := request.Host, "internal.example"; got != want {
		t.Fatalf("request host = %q, want %q", got, want)
	}
}

func TestWithHeadersRecognizesHostCaseInsensitively(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	middleware := WithHeaders{Headers: http.Header{
		"host": []string{"internal.example"},
	}}

	if err := middleware.Request(request); err != nil {
		t.Fatalf("Request() returned an error: %v", err)
	}
	if got, want := request.Host, "internal.example"; got != want {
		t.Fatalf("request host = %q, want %q", got, want)
	}
	if got := request.Header.Get("Host"); got != "" {
		t.Fatalf("Host was written as a regular header: %q", got)
	}
}

func TestWithHeadersPreservesMultipleValues(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://service.example/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	middleware := WithHeaders{Headers: http.Header{
		"X-Scope": []string{"items:read", "items:write"},
	}}

	if err := middleware.Request(request); err != nil {
		t.Fatalf("Request() returned an error: %v", err)
	}
	if got, want := request.Header.Values("X-Scope"), []string{"items:read", "items:write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X-Scope values = %#v, want %#v", got, want)
	}
}

func TestWithHeadersResponseIsNoop(t *testing.T) {
	if err := (&WithHeaders{}).Response(&http.Response{}); err != nil {
		t.Fatalf("Response() returned an error: %v", err)
	}
}
