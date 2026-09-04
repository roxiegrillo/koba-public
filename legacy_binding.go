package koba

import (
	"context"
	"net/http"
	"reflect"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
	"github.com/rs/zerolog"
)

func joinFuncs(fns []InputFunc, ctx context.Context, c *httprouter.Context) ([]reflect.Value, error) {
	values := []reflect.Value{}
	var err error
	for _, fn := range fns {
		values, err = fn(ctx, c, values)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func makePureFunctionHandler(inputType []reflect.Type, methodValue reflect.Value, hasContext bool, name string) func(*httprouter.Context) {
	inputComputingFuncs := computeInput(inputType, hasContext)
	return func(c *httprouter.Context) {
		ctx := c.Request.Context()
		pathParameters := make(map[string]string, len(c.Params))
		for _, parameter := range c.Params {
			pathParameters[parameter.Key] = parameter.Value
		}
		ctx = context.WithValue(ctx, http_common.PathParameters, pathParameters)

		ctxLoggerAttributes := make(map[string]string)
		ctxLoggerAttributes["route_name"] = name
		ctx = SetupLoggerRequestContext(ctx, ctxLoggerAttributes)
		c.Request = c.Request.WithContext(ctx)

		in, err := joinFuncs(inputComputingFuncs, ctx, c)
		if err != nil {
			c.JSON(http.StatusBadRequest, httprouter.H{
				"status":  http.StatusBadRequest,
				"error":   statusCodeAsText(http.StatusBadRequest),
				"message": err.Error(),
			})
			return
		}

		values, iErr := UnBindError(func() []reflect.Value {
			return methodValue.Call(in)
		})
		if iErr.Error != nil {
			errorText, statusCode := getErrorMessageAndStatusCode(iErr.Error)
			logger := zerolog.Ctx(ctx)
			logger.Error().Str("stack", iErr.Stack).Msg(errorText)
			c.JSON(statusCode, httprouter.H{
				"status":  statusCode,
				"error":   statusCodeAsText(statusCode),
				"message": errorText,
			})
		} else {
			if len(values) > 0 {
				var value any
				if values[0].Kind() == reflect.Ptr {
					value = values[0].Elem().Interface()
				} else {
					value = values[0].Interface()
				}
				c.JSON(http.StatusOK, value)
			} else {
				logger := zerolog.Ctx(ctx)
				logger.Info().Msg("nothing to return")
			}
		}
	}
}

type InputFunc func(ctx context.Context, c *httprouter.Context, values []reflect.Value) ([]reflect.Value, error)

func computeInput(inputType []reflect.Type, hasContext bool) []InputFunc {
	funcs := []InputFunc{}
	if inputType == nil || len(inputType) == 0 {
		return funcs
	}

	if hasContext {
		funcs = append(funcs, func(ctx context.Context, c *httprouter.Context, values []reflect.Value) ([]reflect.Value, error) {
			values = append(values, reflect.ValueOf(ctx))
			return values, nil
		})
	}
	if !hasContext || len(inputType) > 1 {
		idx := len(inputType) - 1
		originalType := inputType[idx]

		var t reflect.Type

		if originalType.Kind() == reflect.Ptr {
			t = originalType.Elem()
		} else {
			t = originalType
		}

		funcs = append(funcs, ParseInput(t, originalType))
	}
	return funcs
}

func ParseInput(t reflect.Type, originalType reflect.Type) InputFunc {
	return func(ctx context.Context, c *httprouter.Context, values []reflect.Value) ([]reflect.Value, error) {
		newValue := reflect.New(t)
		if err := c.BindJSON(newValue.Interface()); err != nil {
			return nil, err
		}

		var value reflect.Value
		if originalType.Kind() == reflect.Ptr {
			value = newValue
		} else {
			value = reflect.Indirect(newValue)
		}

		values = append(values, value)

		return values, nil
	}
}
