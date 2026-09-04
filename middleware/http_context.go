package middleware

import (
	"context"
	"net/http"

	http_common "github.com/roxiegrillo/koba/v2/http/common"
)

/*WithHttpContext implements a middleware that adds request.Header and request.URL.Query() objects into the request context.*/
type WithHttpContext struct{}

func (z *WithHttpContext) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	ctx = context.WithValue(ctx, http_common.Headers, request.Header)
	ctx = context.WithValue(ctx, http_common.Query, request.URL.Query())
	ctx = context.WithValue(ctx, http_common.Request, request)
	ctx = context.WithValue(ctx, http_common.ResponseWriter, w)

	*request = *request.WithContext(ctx)
}
