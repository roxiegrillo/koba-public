package koba

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	goerr "github.com/go-errors/errors"
	"github.com/roxiegrillo/koba/v2/errors"
)

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
