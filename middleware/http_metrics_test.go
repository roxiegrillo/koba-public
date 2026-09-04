package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestHTTPMetricsUseServiceAndNormalizedRoute(t *testing.T) {
	const serviceName = "fenix-items-api-metrics-test"
	router := newHTTPMetricsTestRouter(t, serviceName)
	router.GET("/items/:id", func(context *httprouter.Context) {
		context.Status(http.StatusNoContent)
	})

	counter := httpRequests.WithLabelValues(
		serviceName,
		http.MethodGet,
		"/items/:id",
		"204",
	)
	duration := httpRequestDuration.WithLabelValues(
		serviceName,
		http.MethodGet,
		"/items/:id",
		"204",
	)
	beforeRequests := counterValue(t, counter)
	beforeDurations := histogramCount(t, duration)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/item-123", nil))
	if got, want := response.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}

	if got, want := counterValue(t, counter), beforeRequests+1; got != want {
		t.Errorf("request counter = %v, want %v", got, want)
	}
	if got, want := histogramCount(t, duration), beforeDurations+1; got != want {
		t.Errorf("duration sample count = %d, want %d", got, want)
	}
	if got := gaugeValue(t, httpRequestsInFlight.WithLabelValues(
		serviceName,
		http.MethodGet,
		"/items/:id",
	)); got != 0 {
		t.Errorf("in-flight requests = %v, want 0", got)
	}

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	metricNames := make(map[string]struct{}, len(metricFamilies))
	for _, family := range metricFamilies {
		metricNames[family.GetName()] = struct{}{}
	}
	for _, expectedName := range []string{
		"fp_http_requests_total",
		"fp_http_request_duration_seconds",
		"fp_http_requests_in_flight",
	} {
		if _, exists := metricNames[expectedName]; !exists {
			t.Errorf("metric family %q was not gathered", expectedName)
		}
	}
}

func TestHTTPMetricsUseBoundedLabelForUnmatchedRoutes(t *testing.T) {
	const serviceName = "unmatched-route-metrics-test"
	router := newHTTPMetricsTestRouter(t, serviceName)
	counter := httpRequests.WithLabelValues(
		serviceName,
		http.MethodGet,
		unmatchedHTTPRoute,
		"404",
	)
	before := counterValue(t, counter)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing/item-123", nil))
	if got, want := response.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := counterValue(t, counter), before+1; got != want {
		t.Errorf("unmatched request counter = %v, want %v", got, want)
	}
}

func TestHTTPMetricsBoundUnknownMethods(t *testing.T) {
	const serviceName = "method-cardinality-metrics-test"
	router := newHTTPMetricsTestRouter(t, serviceName)
	router.Handle("BREW", "/coffee", func(context *httprouter.Context) {
		context.Status(http.StatusNoContent)
	})
	counter := httpRequests.WithLabelValues(
		serviceName,
		otherHTTPMethod,
		"/coffee",
		"204",
	)
	before := counterValue(t, counter)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("BREW", "/coffee", nil))
	if got, want := response.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := counterValue(t, counter), before+1; got != want {
		t.Errorf("bounded method counter = %v, want %v", got, want)
	}
	if metricHasLabelValue(t, "fp_http_requests_total", "method", "BREW") {
		t.Error("raw unknown method was exported as a Prometheus label")
	}
}

func TestHTTPMetricsRecordRecoveredPanicsAsServerErrors(t *testing.T) {
	const serviceName = "recovered-panic-metrics-test"
	router := newHTTPMetricsTestRouter(t, serviceName)
	router.GET("/panic", func(*httprouter.Context) {
		panic("test panic")
	})
	counter := httpRequests.WithLabelValues(
		serviceName,
		http.MethodGet,
		"/panic",
		"500",
	)
	before := counterValue(t, counter)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if got, want := response.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := counterValue(t, counter), before+1; got != want {
		t.Errorf("server error counter = %v, want %v", got, want)
	}
}

func TestHTTPMetricsTrackNormalizedRouteWhileRequestIsInFlight(t *testing.T) {
	const serviceName = "in-flight-route-metrics-test"
	router := newHTTPMetricsTestRouter(t, serviceName)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	router.GET("/items/:id/wait", func(context *httprouter.Context) {
		close(requestStarted)
		<-releaseRequest
		context.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		router.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodGet, "/items/item-123/wait", nil),
		)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request handler did not start")
	}

	inFlight := httpRequestsInFlight.WithLabelValues(
		serviceName,
		http.MethodGet,
		"/items/:id/wait",
	)
	if got := gaugeValue(t, inFlight); got != 1 {
		t.Errorf("in-flight requests = %v, want 1", got)
	}

	close(releaseRequest)
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("request handler did not finish")
	}
	if got := gaugeValue(t, inFlight); got != 0 {
		t.Errorf("in-flight requests after completion = %v, want 0", got)
	}
}

func TestNewHTTPMetricsMiddlewareRejectsInvalidServiceNames(t *testing.T) {
	for _, serviceName := range []string{"", "fenix items", "FenixItems"} {
		t.Run(serviceName, func(t *testing.T) {
			if _, err := NewHTTPMetricsMiddleware(serviceName); err == nil {
				t.Fatalf("NewHTTPMetricsMiddleware(%q) succeeded", serviceName)
			}
		})
	}
}

func newHTTPMetricsTestRouter(t *testing.T, serviceName string) *httprouter.Engine {
	t.Helper()
	metrics, err := NewHTTPMetricsMiddleware(serviceName)
	if err != nil {
		t.Fatalf("construct HTTP metrics middleware: %v", err)
	}
	router := httprouter.New()
	router.Use(metrics)
	// Recovery must run inside metrics so a recovered panic has a final 500
	// status by the time the metrics defer observes it.
	router.Use(httprouter.Recovery())
	return router
}

type prometheusMetric interface {
	Write(*dto.Metric) error
}

func counterValue(t *testing.T, metric prometheusMetric) float64 {
	t.Helper()
	value := &dto.Metric{}
	if err := metric.Write(value); err != nil {
		t.Fatalf("write counter metric: %v", err)
	}
	return value.GetCounter().GetValue()
}

func histogramCount(t *testing.T, observer interface{}) uint64 {
	t.Helper()
	metric, ok := observer.(prometheusMetric)
	if !ok {
		t.Fatal("histogram observer does not implement prometheus.Metric")
	}
	value := &dto.Metric{}
	if err := metric.Write(value); err != nil {
		t.Fatalf("write histogram metric: %v", err)
	}
	return value.GetHistogram().GetSampleCount()
}

func gaugeValue(t *testing.T, metric prometheusMetric) float64 {
	t.Helper()
	value := &dto.Metric{}
	if err := metric.Write(value); err != nil {
		t.Fatalf("write gauge metric: %v", err)
	}
	return value.GetGauge().GetValue()
}

func metricHasLabelValue(t *testing.T, metricName string, labelName string, labelValue string) bool {
	t.Helper()
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == labelName && label.GetValue() == labelValue {
					return true
				}
			}
		}
	}
	return false
}
