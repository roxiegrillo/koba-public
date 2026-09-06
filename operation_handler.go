package koba

import (
	"context"
	"errors"
	"net/http"

	http_common "github.com/roxiegrillo/koba/v2/http/common"
	internalauthorization "github.com/roxiegrillo/koba/v2/internal/authorization"
	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	internalhttpadapter "github.com/roxiegrillo/koba/v2/internal/transport/httpadapter"
	"github.com/rs/zerolog"
)

// makeOperationHandler is shared by reflected and generated plans. Generated
// invokers contain no HTTP router code; transport parsing, request context enrichment,
// logging, error mapping, and JSON responses remain centralized here.
func makeOperationHandler(operation OperationDescription, invoke OperationInvoker) func(*httprouter.Context) {
	requiredScopes := operation.ResolvedRequiredScopes()
	requiresAuthentication := operation.RequiresAuthentication()
	return func(c *httprouter.Context) {
		ctx := c.Request.Context()
		pathParameters := make(map[string]string, len(c.Params))
		for _, parameter := range c.Params {
			pathParameters[parameter.Key] = parameter.Value
		}
		ctx = context.WithValue(ctx, http_common.PathParameters, pathParameters)

		ctx = SetupLoggerRequestContext(ctx, map[string]string{"route_name": operation.Route})
		c.Request = c.Request.WithContext(ctx)
		var webSocketAcceptance *webSocketAcceptance
		if operation.HasWebSocketTransport() {
			ctx, webSocketAcceptance = contextWithWebSocketAcceptance(
				ctx,
				c.Writer,
				c.Request,
				*operation.Contract.Transport.WebSocket,
			)
			c.Request = c.Request.WithContext(ctx)
		}
		if !internalhttpadapter.AuthorizeOperation(c, requiresAuthentication, requiredScopes) {
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
			if errors.Is(err, internalauthorization.ErrAuthenticationRequired) {
				internalhttpadapter.WriteAuthenticationRequired(c)
				return
			}
			status := bodyDecodeErrorStatus(err)
			c.JSON(status, httprouter.H{
				"status":  status,
				"error":   statusCodeAsText(status),
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
			if webSocketResponseTransitioned(webSocketAcceptance) {
				return
			}
			c.JSON(statusCode, httprouter.H{
				"status":  statusCode,
				"error":   statusCodeAsText(statusCode),
				"message": errorText,
			})
			return
		}
		if invocationError != nil {
			if webSocketResponseTransitioned(webSocketAcceptance) {
				zerolog.Ctx(ctx).Warn().Err(invocationError).Msg("WebSocket operation failed after upgrade attempt")
				return
			}
			if !isInvocationInputError(invocationError) {
				errorText, status := getErrorMessageAndStatusCode(invocationError)
				zerolog.Ctx(ctx).Error().Err(invocationError).Msg("API operation returned an error")
				c.JSON(status, httprouter.H{
					"status":  status,
					"error":   statusCodeAsText(status),
					"message": errorText,
				})
				return
			}
			status := bodyDecodeErrorStatus(invocationError)
			c.JSON(status, httprouter.H{
				"status":  status,
				"error":   statusCodeAsText(status),
				"message": invocationError.Error(),
			})
			return
		}
		if operation.HasWebSocketTransport() {
			attempted, _ := webSocketAcceptance.state()
			if !attempted {
				status := http.StatusInternalServerError
				c.JSON(status, httprouter.H{
					"status":  status,
					"error":   statusCodeAsText(status),
					"message": "WebSocket operation returned without calling koba.AcceptWebSocket",
				})
			}
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

func webSocketResponseTransitioned(acceptance *webSocketAcceptance) bool {
	if acceptance == nil {
		return false
	}
	attempted, _ := acceptance.state()
	return attempted
}

func bodyDecodeErrorStatus(err error) int {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
