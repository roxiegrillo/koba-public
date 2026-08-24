package http_common

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestHeadersFromContext(t *testing.T) {
	stored := http.Header{
		"Authorization": {"Bearer token"},
		"X-Trace-Id":    {"trace-1", "trace-2"},
	}
	ctx := context.WithValue(context.Background(), Headers, stored)

	got := HeadersFromContext(ctx)
	if value := got.Get("Authorization"); value != "Bearer token" {
		t.Fatalf("Authorization = %q, want %q", value, "Bearer token")
	}
	if values := got.Values("X-Trace-ID"); len(values) != 2 || values[0] != "trace-1" || values[1] != "trace-2" {
		t.Fatalf("X-Trace-ID = %#v, want two stored values", values)
	}

	got.Set("Authorization", "changed")
	got["X-Trace-Id"][0] = "changed"
	if value := stored.Get("Authorization"); value != "Bearer token" {
		t.Fatalf("stored Authorization changed through returned map: %q", value)
	}
	if value := stored["X-Trace-Id"][0]; value != "trace-1" {
		t.Fatalf("stored X-Trace-ID changed through returned slice: %q", value)
	}
}

func TestHeadersFromContextWithoutHeaders(t *testing.T) {
	got := HeadersFromContext(context.Background())
	if got == nil {
		t.Fatal("HeadersFromContext returned nil, want an empty header map")
	}
	if len(got) != 0 {
		t.Fatalf("HeadersFromContext returned %#v, want an empty header map", got)
	}
}

func TestQueryFromContext(t *testing.T) {
	stored := url.Values{
		"filter": {"active", "recent"},
		"limit":  {"25"},
	}
	ctx := context.WithValue(context.Background(), Query, stored)

	got := QueryFromContext(ctx)
	if value := got.Get("limit"); value != "25" {
		t.Fatalf("limit = %q, want %q", value, "25")
	}
	if values := got["filter"]; len(values) != 2 || values[0] != "active" || values[1] != "recent" {
		t.Fatalf("filter = %#v, want both stored values", values)
	}

	got.Set("limit", "100")
	got["filter"][0] = "changed"
	if value := stored.Get("limit"); value != "25" {
		t.Fatalf("stored limit changed through returned map: %q", value)
	}
	if value := stored["filter"][0]; value != "active" {
		t.Fatalf("stored filter changed through returned slice: %q", value)
	}
}

func TestQueryFromContextWithoutQuery(t *testing.T) {
	got := QueryFromContext(context.Background())
	if got == nil {
		t.Fatal("QueryFromContext returned nil, want empty values")
	}
	if len(got) != 0 {
		t.Fatalf("QueryFromContext returned %#v, want empty values", got)
	}
}
