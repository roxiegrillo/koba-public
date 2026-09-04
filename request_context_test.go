package koba

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestSetupLoggerRequestContextAddsFields(t *testing.T) {
	var output bytes.Buffer
	logger := zerolog.New(&output)
	ctx := logger.WithContext(context.Background())

	ctx = SetupLoggerRequestContext(ctx, map[string]string{
		"request_id": "request-123",
		"route_name": "/items/:id",
	})
	zerolog.Ctx(ctx).Info().Msg("handled")

	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode log event: %v", err)
	}
	for field, want := range map[string]string{
		"request_id": "request-123",
		"route_name": "/items/:id",
	} {
		if got := event[field]; got != want {
			t.Errorf("log field %q = %#v, want %q", field, got, want)
		}
	}
}
