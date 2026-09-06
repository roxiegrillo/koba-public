package koba

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/anypb"
)

type kafkaTransportEvent struct {
	ID string `json:"id"`
}

type kafkaTransportStatus struct {
	Status string `json:"status"`
}

type mixedKafkaTransportAPI struct {
	received kafkaTransportEvent
	failure  error
}

func (api *mixedKafkaTransportAPI) Consume(_ context.Context, event kafkaTransportEvent) error {
	api.received = event
	return api.failure
}

func (*mixedKafkaTransportAPI) Status() kafkaTransportStatus {
	return kafkaTransportStatus{Status: "ok"}
}

func (*mixedKafkaTransportAPI) Routes() map[string]string {
	return map[string]string{"Status": "/status"}
}

func (*mixedKafkaTransportAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{
		Name: "mixed-transport",
		Operations: map[string]OperationDocumentation{
			"Consume": {
				OperationID: "consumeKafkaTransportEvent",
				Summary:     "Consume an event",
				Tags:        []string{"events"},
			},
			"Status": {OperationID: "getMixedTransportStatus"},
		},
	}
}

func (*mixedKafkaTransportAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "  inventory.items.created  ")
}

func kafkaTestContract(method, topic string) APIContract {
	return APIContract{Operations: map[string]OperationContract{
		method: {Transport: KafkaConsumer(topic)},
	}}
}

func TestKafkaConsumerContractNormalizesTopicWithoutAliasing(t *testing.T) {
	authoring := KafkaConsumer("  inventory.items.created  ")
	normalized, err := normalizeOperationContract(OperationContract{Transport: authoring})
	if err != nil {
		t.Fatalf("normalizeOperationContract() returned an error: %v", err)
	}
	if normalized.Transport.KafkaConsumer == nil {
		t.Fatal("normalized contract has no Kafka consumer transport")
	}
	if got, want := normalized.Transport.KafkaConsumer.Topic, "inventory.items.created"; got != want {
		t.Fatalf("normalized topic = %q, want %q", got, want)
	}
	if normalized.Transport.KafkaConsumer == authoring.KafkaConsumer {
		t.Fatal("normalized Kafka contract aliases the authoring value")
	}
	authoring.KafkaConsumer.Topic = "mutated"
	if got, want := normalized.Transport.KafkaConsumer.Topic, "inventory.items.created"; got != want {
		t.Fatalf("normalized topic changed through alias to %q, want %q", got, want)
	}
}

func TestKafkaConsumerContractRejectsInvalidTopicsAndTransportCollisions(t *testing.T) {
	webSocket := WebSocket(Socket[kafkaTransportEvent, kafkaTransportEvent]())
	tests := []struct {
		name      string
		transport TransportContract
		want      string
	}{
		{name: "empty topic", transport: KafkaConsumer(" \t "), want: "topic is required"},
		{name: "reserved dot topic", transport: KafkaConsumer("."), want: "is reserved"},
		{name: "reserved double-dot topic", transport: KafkaConsumer(".."), want: "is reserved"},
		{name: "unsupported topic character", transport: KafkaConsumer("inventory/items"), want: "unsupported character"},
		{name: "topic too long", transport: KafkaConsumer(strings.Repeat("a", 250)), want: "exceeds 249 characters"},
		{
			name: "multiple transports",
			transport: TransportContract{
				KafkaConsumer: KafkaConsumer("inventory.items").KafkaConsumer,
				WebSocket:     webSocket.WebSocket,
			},
			want: "cannot use both WebSocket and Kafka",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeOperationContract(OperationContract{Transport: test.transport})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeOperationContract() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestDescribeKafkaConsumerInfersTransportAndInvokesErrorReturningFunction(t *testing.T) {
	api := &mixedKafkaTransportAPI{}
	compiled, err := CompileAPI(api)
	if err != nil {
		t.Fatalf("CompileAPI() returned an error: %v", err)
	}
	if got, want := len(compiled.Description.Operations), 2; got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}

	var consumer OperationDescription
	for _, operation := range compiled.Description.Operations {
		if operation.MethodName == "Consume" {
			consumer = operation
		}
	}
	if !consumer.HasKafkaConsumerTransport() {
		t.Fatalf("Consume operation = %#v, want Kafka transport", consumer)
	}
	if got, want := consumer.Contract.Transport.KafkaConsumer.Topic, "inventory.items.created"; got != want {
		t.Fatalf("Kafka topic = %q, want %q", got, want)
	}
	if consumer.Route != "" || consumer.HTTPMethod != "" {
		t.Fatalf("Kafka HTTP binding = %q %q, want none", consumer.HTTPMethod, consumer.Route)
	}
	if got, want := consumer.InputType, reflect.TypeFor[kafkaTransportEvent](); got != want {
		t.Fatalf("Kafka input type = %v, want %v", got, want)
	}
	encoding, err := consumer.ResolvedKafkaMessageEncoding()
	if err != nil {
		t.Fatalf("ResolvedKafkaMessageEncoding() returned an error: %v", err)
	}
	if got, want := encoding, KafkaMessageEncodingJSON; got != want {
		t.Fatalf("Kafka encoding = %q, want %q", got, want)
	}

	event := kafkaTransportEvent{ID: "item-1"}
	result, err := compiled.Invokers["Consume"](Invocation{
		Context:  context.Background(),
		Input:    event,
		HasInput: true,
	})
	if err != nil {
		t.Fatalf("Consume invoker returned an error: %v", err)
	}
	if result.HasValue {
		t.Fatalf("Consume result = %#v, want no response value", result)
	}
	if got, want := api.received, event; got != want {
		t.Fatalf("consumed event = %#v, want %#v", got, want)
	}

	wantFailure := errors.New("retry event")
	api.failure = wantFailure
	_, err = compiled.Invokers["Consume"](Invocation{
		Context:  context.Background(),
		Input:    event,
		HasInput: true,
	})
	if !errors.Is(err, wantFailure) {
		t.Fatalf("Consume invoker error = %v, want %v", err, wantFailure)
	}
}

type kafkaProtobufTransportAPI struct{}

func (*kafkaProtobufTransportAPI) Consume(_ context.Context, _ *anypb.Any) {}

func (*kafkaProtobufTransportAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "protobuf.events")
}

func TestDescribeKafkaConsumerAcceptsProtobufMessageWithoutErrorReturn(t *testing.T) {
	description, err := DescribeReflectedAPI((*kafkaProtobufTransportAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	operation := description.Operations[0]
	encoding, err := operation.ResolvedKafkaMessageEncoding()
	if err != nil {
		t.Fatalf("ResolvedKafkaMessageEncoding() returned an error: %v", err)
	}
	if got, want := encoding, KafkaMessageEncodingProtobuf; got != want {
		t.Fatalf("Kafka encoding = %q, want %q", got, want)
	}
}

type kafkaNoMessageAPI struct{}

func (*kafkaNoMessageAPI) Consume() {}
func (*kafkaNoMessageAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

type kafkaMultipleMessagesAPI struct{}

func (*kafkaMultipleMessagesAPI) Consume(kafkaTransportEvent, kafkaTransportEvent) {}
func (*kafkaMultipleMessagesAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

type kafkaScalarMessageAPI struct{}

func (*kafkaScalarMessageAPI) Consume(string) {}
func (*kafkaScalarMessageAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

type kafkaResponseValueAPI struct{}

func (*kafkaResponseValueAPI) Consume(kafkaTransportEvent) string { return "response" }
func (*kafkaResponseValueAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

type kafkaMultipleReturnsAPI struct{}

func (*kafkaMultipleReturnsAPI) Consume(kafkaTransportEvent) (string, error) { return "response", nil }
func (*kafkaMultipleReturnsAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

type kafkaHTTPRouteAPI struct{}

func (*kafkaHTTPRouteAPI) Consume(kafkaTransportEvent) {}
func (*kafkaHTTPRouteAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

func (*kafkaHTTPRouteAPI) Routes() map[string]string {
	return map[string]string{"Consume": "/consume"}
}

type kafkaHTTPDocumentationAPI struct{}

func (*kafkaHTTPDocumentationAPI) Consume(kafkaTransportEvent) {}
func (*kafkaHTTPDocumentationAPI) APIContract() APIContract {
	return kafkaTestContract("Consume", "events")
}

func (*kafkaHTTPDocumentationAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{
		"Consume": {Method: http.MethodPost},
	}}
}

func TestDescribeKafkaConsumerRejectsInvalidFunctionShapesAndHTTPMetadata(t *testing.T) {
	tests := []struct {
		name string
		api  any
		want string
	}{
		{name: "no message", api: (*kafkaNoMessageAPI)(nil), want: "exactly one message argument"},
		{name: "multiple messages", api: (*kafkaMultipleMessagesAPI)(nil), want: "exactly one message argument"},
		{name: "scalar message", api: (*kafkaScalarMessageAPI)(nil), want: "must be a named struct"},
		{name: "response value", api: (*kafkaResponseValueAPI)(nil), want: "may only return error"},
		{name: "multiple return values", api: (*kafkaMultipleReturnsAPI)(nil), want: "may only return error"},
		{name: "HTTP route", api: (*kafkaHTTPRouteAPI)(nil), want: "cannot declare an HTTP route"},
		{name: "HTTP documentation", api: (*kafkaHTTPDocumentationAPI)(nil), want: "cannot declare HTTP-specific documentation"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeReflectedAPI(test.api)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DescribeReflectedAPI() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestAddAPISkipsKafkaConsumerRoutesInMixedAPI(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&mixedKafkaTransportAPI{})
	router := app.(WithRouter).Router()

	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/status", nil))
	if got, want := status.Code, http.StatusOK; got != want {
		t.Fatalf("GET /status returned %d, want %d: %s", got, want, status.Body.String())
	}

	consumer := httptest.NewRecorder()
	router.ServeHTTP(consumer, httptest.NewRequest(http.MethodPost, "/Consume", nil))
	if got, want := consumer.Code, http.StatusNotFound; got != want {
		t.Fatalf("POST /Consume returned %d, want %d", got, want)
	}
}
