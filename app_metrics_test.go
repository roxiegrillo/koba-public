package koba

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type appMetricsFixtureAPI struct{}

func (*appMetricsFixtureAPI) Get() map[string]string {
	return map[string]string{"status": "ok"}
}

func (*appMetricsFixtureAPI) Routes() map[string]string {
	return map[string]string{"Get": "/items/:id"}
}

func TestWithServiceNameWiresHTTPMetricsIntoApp(t *testing.T) {
	const serviceName = "koba-wiring-test"
	app := MakeRawAppWithOptions(WithServiceName(serviceName))
	app.AddApi(&appMetricsFixtureAPI{})
	router := app.(WithRouter).Router()

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/item-123", nil))
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, response.Body.String())
	}

	wantLabels := map[string]string{
		"service":     serviceName,
		"method":      http.MethodGet,
		"route":       "/items/:id",
		"status_code": "200",
	}
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != "fp_http_requests_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			matches := true
			for name, value := range wantLabels {
				if labels[name] != value {
					matches = false
					break
				}
			}
			if matches && metric.GetCounter().GetValue() == 1 {
				return
			}
		}
	}

	t.Fatalf("fp_http_requests_total has no sample with labels %#v", wantLabels)
}
