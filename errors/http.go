// Package errors builds errors that carry the HTTP status an operation wants
// its failure reported with.
package errors

import (
	"fmt"
	"net/http"
)

// WithSatusCode is implemented by an error that declares its HTTP status. The
// transport looks for it on a returned error, anywhere in its wrap chain, and
// on a panic value. (The misspelling is kept: the name is public API.)
type WithSatusCode interface {
	StatusCode() int
}

// statusError is the error every constructor here returns.
type statusError struct {
	status  int
	message string
}

func (failure *statusError) Error() string   { return failure.message }
func (failure *statusError) StatusCode() int { return failure.status }

// messageOf is the text of cause: a string as is, an error's Error(), and
// anything else formatted with %v.
func messageOf(cause any) string {
	switch value := cause.(type) {
	case string:
		return value
	case error:
		return value.Error()
	default:
		return fmt.Sprint(value)
	}
}

// MakeError returns an error reported with statusCode. cause, normally a
// string or an error, becomes its message.
func MakeError(statusCode int, cause any) error {
	return &statusError{status: statusCode, message: messageOf(cause)}
}

// MakeBadRequestError returns a 400 Bad Request error: the client sent
// something the operation will not process.
func MakeBadRequestError(cause any) error { return MakeError(http.StatusBadRequest, cause) }

// MakeNotFoundError returns a 404 Not Found error: what the client asked for
// does not exist.
func MakeNotFoundError(cause any) error { return MakeError(http.StatusNotFound, cause) }

// MakeTooEarlyError returns a 425 Too Early error: the operation is not ready
// to handle the request yet.
func MakeTooEarlyError(cause any) error { return MakeError(http.StatusTooEarly, cause) }

// MakeInternalServerError returns a 500 Internal Server Error whose message
// the client may read.
func MakeInternalServerError(cause any) error {
	return MakeError(http.StatusInternalServerError, cause)
}

// MakeBadGatewayError returns a 502 Bad Gateway error: a service this one
// depends on answered with something unusable.
func MakeBadGatewayError(cause any) error { return MakeError(http.StatusBadGateway, cause) }
