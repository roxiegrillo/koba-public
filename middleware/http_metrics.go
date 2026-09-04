package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	unmatchedHTTPRoute = "unmatched"
	otherHTTPMethod    = "other"
)

var (
	serviceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	httpRequests       = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "fp",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests handled by a Koba service.",
		},
		[]string{"service", "method", "route", "status_code"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "fp",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds for a Koba service.",
		},
		[]string{"service", "method", "route", "status_code"},
	)
	httpRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "fp",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Current in-flight HTTP requests for a Koba service.",
		},
		[]string{"service", "method", "route"},
	)
	registerHTTPMetricsOnce sync.Once
	registerHTTPMetricsErr  error
)

// NewHTTPMetricsMiddleware builds the standard Koba HTTP
// instrumentation for one service. The service name must be the repository
// name so dashboards can aggregate every service through the same label.
func NewHTTPMetricsMiddleware(serviceName string) (httprouter.HandlerFunc, error) {
	if !serviceNamePattern.MatchString(serviceName) {
		return nil, fmt.Errorf(
			"koba: service name %q must match %s",
			serviceName,
			serviceNamePattern.String(),
		)
	}
	if err := registerHTTPMetrics(); err != nil {
		return nil, err
	}
	return httpMetricsMiddleware(serviceName), nil
}

func registerHTTPMetrics() error {
	registerHTTPMetricsOnce.Do(func() {
		collectors := []struct {
			name      string
			collector prometheus.Collector
		}{
			{name: "fp_http_requests_total", collector: httpRequests},
			{name: "fp_http_request_duration_seconds", collector: httpRequestDuration},
			{name: "fp_http_requests_in_flight", collector: httpRequestsInFlight},
		}
		for _, collector := range collectors {
			if err := prometheus.Register(collector.collector); err != nil {
				registerHTTPMetricsErr = fmt.Errorf(
					"koba: register metric %s: %w",
					collector.name,
					err,
				)
				return
			}
		}
	})
	return registerHTTPMetricsErr
}

func httpMetricsMiddleware(serviceName string) httprouter.HandlerFunc {
	return func(context *httprouter.Context) {
		method := normalizedHTTPMethod(context.Request.Method)
		inFlightRoute := normalizedHTTPRoute(context.FullPath())
		inFlight := httpRequestsInFlight.WithLabelValues(serviceName, method, inFlightRoute)
		started := time.Now()
		inFlight.Inc()

		defer func() {
			inFlight.Dec()
			route := normalizedHTTPRoute(context.FullPath())
			statusCode := strconv.Itoa(context.Writer.Status())
			httpRequests.WithLabelValues(serviceName, method, route, statusCode).Inc()
			httpRequestDuration.
				WithLabelValues(serviceName, method, route, statusCode).
				Observe(time.Since(started).Seconds())
		}()

		context.Next()
	}
}

func normalizedHTTPMethod(method string) string {
	switch method {
	case http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return otherHTTPMethod
	}
}

func normalizedHTTPRoute(route string) string {
	if route == "" {
		return unmatchedHTTPRoute
	}
	segments := strings.Split(route, "/")
	for index, segment := range segments {
		switch {
		case segment == "{$}":
			segments[index] = ""
		case strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "...}"):
			segments[index] = "*" + segment[1:len(segment)-4]
		case strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}"):
			segments[index] = ":" + segment[1:len(segment)-1]
		}
	}
	return strings.Join(segments, "/")
}
