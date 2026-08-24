package koba

import (
	"context"
	stderr "errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/emirpasic/gods/maps/linkedhashmap"
	goerr "github.com/go-errors/errors"
	"github.com/roxiegrillo/koba/v2/errors"
	http_common "github.com/roxiegrillo/koba/v2/http/common"
	"github.com/roxiegrillo/koba/v2/internal/httprouter"
	"github.com/rs/zerolog"
)

func isNotExportable(method reflect.Method) bool {
	if method.Name == "Routes" {
		return true
	}
	if method.Name == "APIDocumentation" &&
		method.Type.NumIn() == 1 &&
		method.Type.NumOut() == 1 &&
		method.Type.Out(0) == reflect.TypeFor[APIDocumentation]() {
		return true
	}
	return method.Name == "KobaCompiledAPI" &&
		method.Type.NumIn() == 1 &&
		method.Type.NumOut() == 1 &&
		method.Type.Out(0) == reflect.TypeFor[CompiledAPI]()
}

func SetupLoggerRequestContext(ctx context.Context, values map[string]string) context.Context {
	logger := zerolog.Ctx(ctx).With().Logger()
	logger.With().Fields(values)
	ctx = logger.WithContext(ctx)
	return ctx
}

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

func makePureFunctionHandler(inputType []reflect.Type, methodValue reflect.Value, hasContext bool, name string) httprouter.HandlerFunc {
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

// makeOperationHandler is shared by reflected and generated plans. Generated
// invokers contain no router code; transport parsing, request context
// enrichment, logging, error mapping, and JSON responses remain centralized
// here.
func makeOperationHandler(operation OperationDescription, invoke OperationInvoker) httprouter.HandlerFunc {
	return func(c *httprouter.Context) {
		ctx := c.Request.Context()
		pathParameters := make(map[string]string, len(c.Params))
		for _, parameter := range c.Params {
			pathParameters[parameter.Key] = parameter.Value
		}
		ctx = context.WithValue(ctx, http_common.PathParameters, pathParameters)

		ctx = SetupLoggerRequestContext(ctx, map[string]string{"route_name": operation.Route})
		c.Request = c.Request.WithContext(ctx)

		result, invocationError, panicError := invokeOperation(invoke, Invocation{
			Context: ctx,
			DecodeBody: func(target any) error {
				return c.BindJSON(target)
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
			c.JSON(http.StatusBadRequest, httprouter.H{
				"status":  http.StatusBadRequest,
				"error":   statusCodeAsText(http.StatusBadRequest),
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

func invokeOperation(invoke OperationInvoker, invocation Invocation) (result InvocationResult, invocationError error, panicError ErrorCtx) {
	defer func() {
		panicError.Error = recover()
		if panicError.Error != nil {
			panicError.Stack = goerr.Wrap(panicError.Error, 2).ErrorStack()
		}
	}()
	result, invocationError = invoke(invocation)
	return
}

func statusCodeAsText(statusCode int) string {
	return strings.Replace(strings.ToLower(http.StatusText(statusCode)), " ", "_", -1)
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

func getErrorMessageAndStatusCode(err interface{}) (errorText string, statusCode int) {
	if status, ok := err.(errors.WithSatusCode); ok {
		statusCode = status.StatusCode()
	} else {
		statusCode = http.StatusInternalServerError
	}

	if asError, ok := err.(error); ok {
		errorText = asError.Error()
	} else {
		errorText = fmt.Sprintf("unknown error: %v", err)
	}

	return
}

type ErrorCtx struct {
	Error interface{}
	Stack string
}

func UnBindError(closure func() []reflect.Value) (values []reflect.Value, err ErrorCtx) {
	defer func() {
		err.Error = recover()
		if err.Error != nil {
			err.Stack = goerr.Wrap(err, 2).ErrorStack()
		}
	}()
	values = closure()
	return
}

func addMapMiddleware(r *regexp.Regexp, m *linkedhashmap.Map, handlers ...http.Handler) {
	var newHandlers []http.Handler
	if h, ok := m.Get(r); ok {
		hOld := h.([]http.Handler)
		newHandlers = append(newHandlers, hOld...)
	}
	newHandlers = append(newHandlers, handlers...)
	m.Put(r, newHandlers)
}

func asRouterHandler(h http.Handler) httprouter.HandlerFunc {
	return func(c *httprouter.Context) {
		// Save the initial status before handler execution
		initialStatus := c.Writer.Status()
		initialSize := c.Writer.Size()

		// Execute the handler
		h.ServeHTTP(c.Writer, c.Request)

		// Check if handler wrote to the response or set any status
		finalStatus := c.Writer.Status()
		finalSize := c.Writer.Size()

		// If status changed or content was written, stop the chain
		if finalStatus != initialStatus || finalSize > initialSize {
			return
		}
		c.Next()
	}
}

func GetInputOutputTypes(method reflect.Method) ([]reflect.Type, []reflect.Type) {
	numIn := method.Type.NumIn()
	numOut := method.Type.NumOut()

	var inputType []reflect.Type

	for i := 1; i < numIn; i++ {
		inputType = append(inputType, method.Type.In(i))
	}

	var outputType []reflect.Type

	for i := 0; i < numOut; i++ {
		outputType = append(outputType, method.Type.Out(i))
	}

	return inputType, outputType
}

func HasContext(types []reflect.Type) bool {
	if len(types) == 0 {
		return false
	}
	lastType := types[0]
	request := reflect.New(lastType).Interface()
	_, ok := request.(*context.Context)

	return ok
}

func isPost(inputTypes []reflect.Type, outputTypes []reflect.Type) (bool, error) {
	hasCtx := HasContext(inputTypes)
	numIn := len(inputTypes)
	numOut := len(outputTypes)
	base := 0
	if hasCtx {
		base = 1
	}
	if numIn == (base+1) && (numOut == 0 || numOut == 1) {
		return true, nil
	} else if numIn == (base) && (numOut == 0 || numOut == 1) {
		return false, nil
	} else {
		return false, stderr.New("Invalid args")
	}
}

func addMapRouter(name string, router *httprouter.RouterGroup, m *linkedhashmap.Map) {
	m.Each(func(key interface{}, value interface{}) {
		r := key.(*regexp.Regexp)
		if r.MatchString(name) {
			middleware := value.([]http.Handler)
			useWithRouter(router, middleware...)
		}
	})
}

func useWithRouter(router httprouter.IRouter, handlers ...http.Handler) {
	for _, handle := range handlers {
		router.Use(asRouterHandler(handle))
	}
}
