package koba

import (
	"context"
	"net/http"

	"github.com/braintree/manners"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	"github.com/rs/zerolog/log"

	internalhealth "github.com/roxiegrillo/koba/v2/internal/health"
	internalhttpadapter "github.com/roxiegrillo/koba/v2/internal/transport/httpadapter"
	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

func (app *kobaApp) startApp(config appConfig) error {
	var metrics httprouter.HandlerFunc
	if config.serviceNameSet {
		var err error
		metrics, err = appmiddleware.NewHTTPMetricsMiddleware(config.serviceName)
		if err != nil {
			return err
		}
	}

	app.r = httprouter.New()
	if !config.defaultProbesDisabled {
		probeHandler, err := internalhealth.NewHandler(internalhealth.Config{
			Checks:  config.readinessChecks,
			Timeout: config.readinessTimeout,
			OnFailure: func(_ context.Context, name string, err error) {
				log.Error().Str("readiness_check", name).Err(err).Msg("service is not ready")
			},
		})
		if err != nil {
			return err
		}
		app.r.Use(internalhttpadapter.DefaultProbes(probeHandler))
	}
	app.r.Use(httprouter.Logger())
	if metrics != nil {
		app.r.Use(metrics)
	}
	// Recovery runs inside the metrics middleware so recovered panics are
	// recorded with their final HTTP 500 response status.
	app.r.Use(httprouter.Recovery())
	app.middleware = internalhttpadapter.NewMiddlewareRegistry()
	app.authenticationMiddleware = config.authenticationMiddleware
	app.defaultProbesDisabled = config.defaultProbesDisabled
	return nil
}

/*Run executes services that expose the api*/
func (app *kobaApp) Run() {
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()
	if err := manners.ListenAndServe(":8080", app.r); err != nil {
		panic(err.Error())
	}
}
