package koba

import (
	"context"
	"errors"
	"net/http"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
	"github.com/rs/zerolog"
)

// makeOperationHandler is shared by reflected and generated plans. Generated
// invokers contain no Gin code; transport parsing, request context enrichment,
// logging, error mapping, and JSON responses remain centralized here.
func makeOperationHandler(operation OperationDescription, invoke OperationInvoker) func(*httprouter.Context) {
	requiredScopes := operation.ResolvedRequiredScopes()
	return func(c *httprouter.Context) {
		ctx := c.Request.Context()
		pathParameters := make(map[string]string, len(c.Params))
		for _, parameter := range c.Params {
			pathParameters[parameter.Key] = parameter.Value
		}
		ctx = context.WithValue(ctx, http_common.PathParameters, pathParameters)

		ctx = SetupLoggerRequestContext(ctx, map[string]string{"route_name": operation.Route})
		c.Request = c.Request.WithContext(ctx)
		if !authorizeOperation(c, requiredScopes) {
			return
		}
		var constructedInput any
		var err error
		hasConstructedInput := len(operation.RequestBindings) > 0
		if hasConstructedInput {
			constructedInput, err = constructOperationRequest(operation, c)
		} else {
			var inputs OperationInputs
			inputs, err = bindHTTPInputs(operation, c)
			ctx = ContextWithOperationInputs(ctx, inputs)
			c.Request = c.Request.WithContext(ctx)
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, httprouter.H{
				"status":  http.StatusBadRequest,
				"error":   statusCodeAsText(http.StatusBadRequest),
				"message": err.Error(),
			})
			return
		}

		result, invocationError, panicError := invokeOperation(invoke, Invocation{
			Context:  ctx,
			Input:    constructedInput,
			HasInput: hasConstructedInput,
			DecodeBody: func(target any) error {
				// ShouldBindJSON returns the decode error without committing a
				// response. The operation handler remains the single owner of the
				// status code and JSON error envelope.
				return c.ShouldBindJSON(target)
			},
		})
		if panicError.Error != nil {
			errorText, statusCode := getErrorMessageAndStatusCode(panicError.Error)
			logger := zerolog.Ctx(ctx)
			logger.Error().Str("stack", panicError.Stack).Msg(errorText)
			c.JSON(statusCode, httprouter.H{
				"status":  statusCode,
				"error":   statusCodeAsText(statusCode),
				"message": errorText,
			})
			return
		}
		if invocationError != nil {
			status := bodyDecodeErrorStatus(invocationError)
			c.JSON(status, httprouter.H{
				"status":  status,
				"error":   statusCodeAsText(status),
				"message": invocationError.Error(),
			})
			return
		}
		status := operation.ResolvedSuccessStatus()
		if status == http.StatusNoContent || status == http.StatusResetContent {
			c.Status(status)
			return
		}
		if result.HasValue {
			c.JSON(status, result.Value)
			return
		}
		c.Status(status)
		logger := zerolog.Ctx(ctx)
		logger.Info().Msg("nothing to return")
	}
}

func bodyDecodeErrorStatus(err error) int {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
