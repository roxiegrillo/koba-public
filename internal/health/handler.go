// Package health implements the transport-level health and readiness probes
// shared by every Koba app.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

const (
	HealthPath              = "/healthz"
	ReadinessPath           = "/readyz"
	DefaultReadinessTimeout = 2 * time.Second
)

// Check is one named dependency boundary required for readiness.
type Check struct {
	Name string
	Run  func(context.Context) error
}

// Config controls readiness evaluation and internal failure reporting.
type Config struct {
	Checks    []Check
	Timeout   time.Duration
	OnFailure func(context.Context, string, error)
}

// Handler serves the two standard operational endpoints.
type Handler struct {
	checks    []Check
	timeout   time.Duration
	onFailure func(context.Context, string, error)
	// runSlot bounds non-cooperative checks to one in-flight evaluation. A
	// timed-out check keeps the slot until it actually returns.
	runSlot chan struct{}
}

type readinessResult struct {
	name string
	err  error
}

// NewHandler validates and constructs an operational probe handler.
func NewHandler(config Config) (*Handler, error) {
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("koba/health: readiness timeout must be positive")
	}

	checks := make([]Check, 0, len(config.Checks))
	seen := make(map[string]struct{}, len(config.Checks))
	for index, check := range config.Checks {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			return nil, fmt.Errorf("koba/health: readiness check %d has an empty name", index+1)
		}
		if check.Run == nil {
			return nil, fmt.Errorf("koba/health: readiness check %q is nil", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("koba/health: duplicate readiness check %q", name)
		}
		seen[name] = struct{}{}
		checks = append(checks, Check{Name: name, Run: check.Run})
	}

	return &Handler{
		checks:    checks,
		timeout:   config.Timeout,
		onFailure: config.OnFailure,
		runSlot:   make(chan struct{}, 1),
	}, nil
}

// Handles reports whether request targets a default probe. Only GET is
// intercepted; other methods remain available to the application router.
func (handler *Handler) Handles(request *http.Request) bool {
	if handler == nil || request == nil || request.Method != http.MethodGet {
		return false
	}
	return request.URL.Path == HealthPath || request.URL.Path == ReadinessPath
}

// ServeHTTP serves a request previously accepted by Handles.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")

	switch request.URL.Path {
	case HealthPath:
		writeStatus(writer, http.StatusOK, "ok")
	case ReadinessPath:
		if name, err := handler.checkReadiness(request.Context()); err != nil {
			if handler.onFailure != nil {
				handler.onFailure(request.Context(), name, err)
			}
			writeStatus(writer, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeStatus(writer, http.StatusOK, "ready")
	default:
		http.NotFound(writer, request)
	}
}

func (handler *Handler) checkReadiness(requestContext context.Context) (string, error) {
	if len(handler.checks) == 0 {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(requestContext, handler.timeout)
	defer cancel()

	select {
	case handler.runSlot <- struct{}{}:
	case <-ctx.Done():
		return "readiness", ctx.Err()
	}

	result := make(chan readinessResult, 1)
	go func() {
		defer func() { <-handler.runSlot }()
		name, err := handler.runChecks(ctx)
		result <- readinessResult{name: name, err: err}
	}()

	select {
	case completed := <-result:
		return completed.name, completed.err
	case <-ctx.Done():
		return "readiness", ctx.Err()
	}
}

func (handler *Handler) runChecks(ctx context.Context) (string, error) {
	for _, check := range handler.checks {
		if err := runCheck(ctx, check.Run); err != nil {
			return check.Name, err
		}
		if err := ctx.Err(); err != nil {
			return check.Name, err
		}
	}
	return "", nil
}

func runCheck(ctx context.Context, check func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("readiness check panicked: %v", recovered)
		}
	}()
	return check(ctx)
}

func writeStatus(writer http.ResponseWriter, statusCode int, status string) {
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(fbtypes.ProbeStatus{Status: status})
}
