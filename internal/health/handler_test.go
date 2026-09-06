package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandlerServesDefaultProbes(t *testing.T) {
	handler, err := NewHandler(Config{Timeout: DefaultReadinessTimeout})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		body string
	}{
		{path: HealthPath, body: `{"status":"ok"}`},
		{path: ReadinessPath, body: `{"status":"ready"}`},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if !handler.Handles(request) {
				t.Fatalf("Handles(GET %s) = false", test.path)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != test.body {
				t.Fatalf("response = %d %q, want 200 %s", response.Code, response.Body.String(), test.body)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
		})
	}

	if handler.Handles(httptest.NewRequest(http.MethodPost, HealthPath, nil)) {
		t.Fatal("Handles(POST /healthz) = true")
	}
	if handler.Handles(httptest.NewRequest(http.MethodGet, "/other", nil)) {
		t.Fatal("Handles(GET /other) = true")
	}
}

func TestHandlerReportsReadinessFailureWithoutLeakingCause(t *testing.T) {
	var observedName string
	var observedError error
	handler, err := NewHandler(Config{
		Timeout: DefaultReadinessTimeout,
		Checks: []Check{{
			Name: "postgres",
			Run:  func(context.Context) error { return errors.New("database password leaked") },
		}},
		OnFailure: func(_ context.Context, name string, err error) {
			observedName = name
			observedError = err
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
	if response.Code != http.StatusServiceUnavailable || strings.TrimSpace(response.Body.String()) != `{"status":"not_ready"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "password") {
		t.Fatalf("response leaked readiness failure: %s", response.Body.String())
	}
	if observedName != "postgres" || observedError == nil || !strings.Contains(observedError.Error(), "password") {
		t.Fatalf("observer = %q, %v", observedName, observedError)
	}
}

func TestHandlerBoundsReadinessAndRecoversPanics(t *testing.T) {
	for _, test := range []struct {
		name    string
		timeout time.Duration
		check   func(context.Context) error
	}{
		{
			name:    "timeout",
			timeout: 10 * time.Millisecond,
			check: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
		{
			name:    "panic",
			timeout: DefaultReadinessTimeout,
			check:   func(context.Context) error { panic("boom") },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewHandler(Config{
				Timeout: test.timeout,
				Checks:  []Check{{Name: test.name, Run: test.check}},
			})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", response.Code)
			}
		})
	}
}

func TestHandlerBoundsNonCooperativeChecksAndPreventsAccumulation(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	defer close(release)

	handler, err := NewHandler(Config{
		Timeout: 10 * time.Millisecond,
		Checks: []Check{{
			Name: "stuck",
			Run: func(context.Context) error {
				calls.Add(1)
				<-release
				return nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		started := time.Now()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d status = %d, want 503", attempt+1, response.Code)
		}
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("attempt %d exceeded bounded response time: %s", attempt+1, elapsed)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("non-cooperative check calls = %d, want 1", got)
	}
}

func TestNewHandlerValidatesConfiguration(t *testing.T) {
	for _, test := range []struct {
		name   string
		config Config
	}{
		{name: "timeout", config: Config{}},
		{name: "empty name", config: Config{Timeout: time.Second, Checks: []Check{{Run: func(context.Context) error { return nil }}}}},
		{name: "nil check", config: Config{Timeout: time.Second, Checks: []Check{{Name: "db"}}}},
		{name: "duplicate", config: Config{Timeout: time.Second, Checks: []Check{
			{Name: "db", Run: func(context.Context) error { return nil }},
			{Name: "db", Run: func(context.Context) error { return nil }},
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewHandler(test.config); err == nil {
				t.Fatal("NewHandler() returned nil error")
			}
		})
	}
}
