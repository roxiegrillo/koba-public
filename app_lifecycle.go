package koba

import (
	"net/http"

	"github.com/braintree/manners"
	"github.com/emirpasic/gods/maps/linkedhashmap"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	app.r.Use(httprouter.Logger())
	if metrics != nil {
		app.r.Use(metrics)
	}
	// Recovery runs inside the metrics middleware so recovered panics are
	// recorded with their final HTTP 500 response status.
	app.r.Use(httprouter.Recovery())
	app.middleware = linkedhashmap.New()
	app.authenticationMiddleware = config.authenticationMiddleware
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
