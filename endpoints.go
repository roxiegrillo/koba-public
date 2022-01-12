package koba

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"

	"github.com/roxiegrillo/koba/v2/internal/httprouter"
)

// endpoint is one exported method of an API, served at a route.
type endpoint struct {
	name       string
	route      string
	httpMethod string
	method     reflect.Method
	call       reflect.Value
}

// endpointsOf lists the endpoints api's exported methods make, in the order
// reflect reports them. A method is served when it takes at most one
// parameter, the request, and returns at most one value, the response; any
// other shape is an error, so that a method nobody could call is noticed when
// the API is registered.
func endpointsOf(api any) ([]endpoint, error) {
	apiType := reflect.TypeOf(api)
	if apiType == nil {
		return nil, errors.New("koba: an API is a value, not nil")
	}
	var routes Routes
	if chooser, ok := api.(WithRoutesAPI); ok {
		routes = chooser.Routes()
	}
	receiver := reflect.ValueOf(api)
	endpoints := make([]endpoint, 0, apiType.NumMethod())
	for index := 0; index < apiType.NumMethod(); index++ {
		method := apiType.Method(index)
		if method.Name == "Routes" {
			continue
		}
		if err := checkShape(method); err != nil {
			return nil, err
		}
		route, chosen := routes[method.Name]
		if !chosen {
			route = RouteOf(method.Name)
		}
		endpoints = append(endpoints, endpoint{
			name:       method.Name,
			route:      route,
			httpMethod: httpMethodOf(method),
			method:     method,
			call:       receiver.Method(index),
		})
	}
	return endpoints, nil
}

// checkShape says whether an endpoint can serve method. The receiver is the
// method's first parameter; the request, when there is one, is the second.
func checkShape(method reflect.Method) error {
	if method.Type.NumIn() > 2 {
		return fmt.Errorf("koba: method %s takes %d parameters; an endpoint supplies at most one, the request", method.Name, method.Type.NumIn()-1)
	}
	if method.Type.NumIn() == 2 && method.Type.In(1).Kind() == reflect.Pointer {
		return fmt.Errorf("koba: method %s takes a pointer; an endpoint decodes the request into a value", method.Name)
	}
	if method.Type.NumOut() > 1 {
		return fmt.Errorf("koba: method %s returns %d values; an endpoint responds with at most one", method.Name, method.Type.NumOut())
	}
	return nil
}

// httpMethodOf is the HTTP method an endpoint answers to: POST when the
// method takes a request, which arrives as the body, and GET when it does
// not.
func httpMethodOf(method reflect.Method) string {
	if method.Type.NumIn() > 1 {
		return http.MethodPost
	}
	return http.MethodGet
}

// RouteOf is the route an endpoint gets from the name of its method: the
// name in lower case, with a dash between words. "DeclarePoll" is served at
// "/declare-poll" and "ListHTTPStatuses" at "/list-http-statuses".
func RouteOf(methodName string) string {
	letters := []rune(methodName)
	var route strings.Builder
	route.WriteByte('/')
	for index, letter := range letters {
		if index > 0 && startsWord(letters, index) {
			route.WriteByte('-')
		}
		route.WriteRune(unicode.ToLower(letter))
	}
	return route.String()
}

// startsWord reports whether the upper-case letter at index begins a word:
// it follows a lower-case letter or a digit, or it is the last capital of an
// acronym before a lower-case letter.
func startsWord(letters []rune, index int) bool {
	if !unicode.IsUpper(letters[index]) {
		return false
	}
	previous := letters[index-1]
	if !unicode.IsUpper(previous) {
		return true
	}
	return index+1 < len(letters) && unicode.IsLower(letters[index+1])
}

// handler serves the endpoint: it decodes the request into the method's
// parameter, calls the method and writes what it returned as JSON, or 204 No
// Content when it returns nothing. A body that does not decode is answered
// with 400 Bad Request; a panic in the method is answered as a failure.
func (served endpoint) handler() httprouter.HandlerFunc {
	return func(context *httprouter.Context) {
		arguments, err := served.arguments(context)
		if err != nil {
			writeRejection(context, err)
			return
		}
		results, failure := invoke(served.call, arguments)
		if failure != nil {
			writeFailure(context, failure)
			return
		}
		writeResults(context, results)
	}
}

// arguments builds the values the method is called with, one per parameter
// after the receiver: the request, decoded from the JSON body.
func (served endpoint) arguments(context *httprouter.Context) ([]reflect.Value, error) {
	signature := served.method.Type
	arguments := make([]reflect.Value, 0, signature.NumIn()-1)
	for index := 1; index < signature.NumIn(); index++ {
		request := reflect.New(signature.In(index))
		if err := context.ShouldBindJSON(request.Interface()); err != nil {
			return nil, fmt.Errorf("request body: %w", err)
		}
		arguments = append(arguments, request.Elem())
	}
	return arguments, nil
}

// invoke calls the method and reports a panic as the failure instead of
// letting it escape.
func invoke(call reflect.Value, arguments []reflect.Value) (results []reflect.Value, failure any) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = recovered
		}
	}()
	return call.Call(arguments), nil
}

func writeResults(context *httprouter.Context, results []reflect.Value) {
	if len(results) == 0 {
		context.Status(http.StatusNoContent)
		return
	}
	context.JSON(http.StatusOK, results[0].Interface())
}

// writeRejection answers a request the endpoint could not read.
func writeRejection(context *httprouter.Context, err error) {
	writeError(context, http.StatusBadRequest, err.Error())
}

// writeFailure answers a call that failed.
func writeFailure(context *httprouter.Context, failure any) {
	writeError(context, http.StatusInternalServerError, messageOf(failure))
}

// writeError answers with a JSON body that names the status and carries the
// message.
func writeError(context *httprouter.Context, status int, message string) {
	context.JSON(status, httprouter.H{
		"status":  status,
		"error":   statusName(status),
		"message": message,
	})
}

// statusName is the status in the form an error body carries: its standard
// text in lower case with underscores, "not_found" for 404.
func statusName(status int) string {
	return strings.ReplaceAll(strings.ToLower(http.StatusText(status)), " ", "_")
}

// messageOf is the text a failure is reported with: an error's Error, and
// anything else printed.
func messageOf(failure any) string {
	if err, ok := failure.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(failure)
}
