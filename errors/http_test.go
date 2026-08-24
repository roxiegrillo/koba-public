package errors

import (
	"errors"
	"net/http"
	"testing"
)

type stringer struct{}

func (stringer) String() string { return "described" }

func TestMakeErrorKeepsStatusAndMessage(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause any
		want  string
	}{
		{name: "string", cause: "request conflicts", want: "request conflicts"},
		{name: "error", cause: errors.New("request conflicts"), want: "request conflicts"},
		{name: "number", cause: 42, want: "42"},
		{name: "stringer", cause: stringer{}, want: "described"},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := MakeError(http.StatusConflict, test.cause)
			carrier, ok := failure.(WithSatusCode)
			if !ok {
				t.Fatalf("%T does not implement WithSatusCode", failure)
			}
			if got := carrier.StatusCode(); got != http.StatusConflict {
				t.Fatalf("StatusCode() = %d, want %d", got, http.StatusConflict)
			}
			if got := failure.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNamedConstructorsUseTheirStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		build  func(any) error
		status int
	}{
		{name: "bad request", build: MakeBadRequestError, status: http.StatusBadRequest},
		{name: "not found", build: MakeNotFoundError, status: http.StatusNotFound},
		{name: "too early", build: MakeTooEarlyError, status: http.StatusTooEarly},
		{name: "internal server error", build: MakeInternalServerError, status: http.StatusInternalServerError},
		{name: "bad gateway", build: MakeBadGatewayError, status: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := test.build("failure")
			var carrier WithSatusCode
			if !errors.As(failure, &carrier) {
				t.Fatalf("%T does not implement WithSatusCode", failure)
			}
			if got := carrier.StatusCode(); got != test.status {
				t.Fatalf("StatusCode() = %d, want %d", got, test.status)
			}
			if got := failure.Error(); got != "failure" {
				t.Fatalf("Error() = %q, want %q", got, "failure")
			}
		})
	}
}
