package workers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	compiledBindingJSONTopic     = "koba.test.json"
	compiledBindingProtobufTopic = "koba.test.protobuf"
)

func newCompiledBindingBinder(t *testing.T, topics ...string) Binder {
	t.Helper()
	readers := make(map[string]*kafka.Reader, len(topics))
	for _, topic := range topics {
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers: []string{"localhost:9092"},
			Topic:   topic,
			GroupID: "koba-test",
		})
		t.Cleanup(func() { _ = reader.Close() })
		readers[topic] = reader
	}
	return Binder{Readers: readers}
}

type compiledBindingContextKey struct{}

type compiledBindingJSONMessage struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

type compiledBindingJSONAPI struct {
	contextValue string
	message      compiledBindingJSONMessage
	returnError  error
	panicValue   any
}

func (api *compiledBindingJSONAPI) Consume(ctx context.Context, message compiledBindingJSONMessage) error {
	api.contextValue, _ = ctx.Value(compiledBindingContextKey{}).(string)
	api.message = message
	if api.panicValue != nil {
		panic(api.panicValue)
	}
	return api.returnError
}

func (*compiledBindingJSONAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Consume": {Transport: fb.KafkaConsumer(compiledBindingJSONTopic)},
	}}
}

func TestBinderBindAPIRegistersJSONConsumerAndPreservesContext(t *testing.T) {
	api := &compiledBindingJSONAPI{}
	binder := newCompiledBindingBinder(t, compiledBindingJSONTopic)
	if err := binder.BindAPI(api); err != nil {
		t.Fatalf("BindAPI() error = %v", err)
	}

	handler, found := binder.Handlers[compiledBindingJSONTopic]
	if !found {
		t.Fatalf("registered topics = %#v, want %q", binder.Handlers, compiledBindingJSONTopic)
	}
	ctx := context.WithValue(context.Background(), compiledBindingContextKey{}, "request-context")
	if err := handler(ctx, &kafka.Message{Value: []byte(`{"id":"item-123","count":7}`)}); err != nil {
		t.Fatalf("handler() error = %v", err)
	}

	if got, want := api.message, (compiledBindingJSONMessage{ID: "item-123", Count: 7}); !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded message = %#v, want %#v", got, want)
	}
	if got, want := api.contextValue, "request-context"; got != want {
		t.Fatalf("context value = %q, want %q", got, want)
	}
}

type compiledBindingProtobufAPI struct {
	message string
}

func (api *compiledBindingProtobufAPI) Consume(message *wrapperspb.StringValue) {
	api.message = message.Value
}

func (*compiledBindingProtobufAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Consume": {Transport: fb.KafkaConsumer(compiledBindingProtobufTopic)},
	}}
}

func TestBinderBindAPIDecodesProtobufConsumer(t *testing.T) {
	api := &compiledBindingProtobufAPI{}
	binder := newCompiledBindingBinder(t, compiledBindingProtobufTopic)
	if err := binder.BindAPI(api); err != nil {
		t.Fatalf("BindAPI() error = %v", err)
	}
	payload, err := proto.Marshal(wrapperspb.String("message-123"))
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}

	if err := binder.Handlers[compiledBindingProtobufTopic](context.Background(), &kafka.Message{Value: payload}); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if got, want := api.message, "message-123"; got != want {
		t.Fatalf("decoded protobuf value = %q, want %q", got, want)
	}
}

func TestBinderBindAPIHandlerPropagatesReturnedError(t *testing.T) {
	want := errors.New("retry message")
	api := &compiledBindingJSONAPI{returnError: want}
	binder := newCompiledBindingBinder(t, compiledBindingJSONTopic)
	if err := binder.BindAPI(api); err != nil {
		t.Fatalf("BindAPI() error = %v", err)
	}

	err := binder.Handlers[compiledBindingJSONTopic](context.Background(), &kafka.Message{Value: []byte(`{"id":"item-123"}`)})
	if !errors.Is(err, want) {
		t.Fatalf("handler() error = %v, want %v", err, want)
	}
}

func TestBinderBindAPIHandlerRecoversPanics(t *testing.T) {
	tests := []struct {
		name       string
		panicValue any
		want       string
	}{
		{name: "error", panicValue: errors.New("consumer error panic"), want: "consumer error panic"},
		{name: "string", panicValue: "consumer string panic", want: "consumer string panic"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &compiledBindingJSONAPI{panicValue: test.panicValue}
			binder := newCompiledBindingBinder(t, compiledBindingJSONTopic)
			if err := binder.BindAPI(api); err != nil {
				t.Fatalf("BindAPI() error = %v", err)
			}

			err := binder.Handlers[compiledBindingJSONTopic](context.Background(), &kafka.Message{Value: []byte(`{"id":"item-123"}`)})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("handler() error = %v, want panic containing %q", err, test.want)
			}
		})
	}
}

func TestBinderBindAPIHandlerRejectsInvalidMessages(t *testing.T) {
	tests := []struct {
		name    string
		api     any
		topic   string
		message *kafka.Message
	}{
		{
			name:    "nil Kafka message",
			api:     &compiledBindingJSONAPI{},
			topic:   compiledBindingJSONTopic,
			message: nil,
		},
		{
			name:    "invalid JSON",
			api:     &compiledBindingJSONAPI{},
			topic:   compiledBindingJSONTopic,
			message: &kafka.Message{Value: []byte(`{"id":`)},
		},
		{
			name:    "invalid protobuf",
			api:     &compiledBindingProtobufAPI{},
			topic:   compiledBindingProtobufTopic,
			message: &kafka.Message{Value: []byte{0xff}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binder := newCompiledBindingBinder(t, test.topic)
			if err := binder.BindAPI(test.api); err != nil {
				t.Fatalf("BindAPI() error = %v", err)
			}
			if err := binder.Handlers[test.topic](context.Background(), test.message); err == nil {
				t.Fatal("handler() error = nil, want payload error")
			}
		})
	}
}

type compiledBindingMultipleAPI struct {
	first  string
	second string
}

func (api *compiledBindingMultipleAPI) First(message compiledBindingJSONMessage) {
	api.first = message.ID
}

func (api *compiledBindingMultipleAPI) Second(message compiledBindingJSONMessage) {
	api.second = message.ID
}

func (*compiledBindingMultipleAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"First":  {Transport: fb.KafkaConsumer("koba.test.first")},
		"Second": {Transport: fb.KafkaConsumer("koba.test.second")},
	}}
}

func TestBinderBindAPIKeepsOperationAndTopicBindingsIndependent(t *testing.T) {
	api := &compiledBindingMultipleAPI{}
	binder := newCompiledBindingBinder(
		t,
		"koba.test.first",
		"koba.test.second",
	)
	if err := binder.BindAPI(api); err != nil {
		t.Fatalf("BindAPI() error = %v", err)
	}

	if err := binder.Handlers["koba.test.first"](
		context.Background(),
		&kafka.Message{Value: []byte(`{"id":"first-message"}`)},
	); err != nil {
		t.Fatalf("first handler error = %v", err)
	}
	if err := binder.Handlers["koba.test.second"](
		context.Background(),
		&kafka.Message{Value: []byte(`{"id":"second-message"}`)},
	); err != nil {
		t.Fatalf("second handler error = %v", err)
	}

	if got, want := api.first, "first-message"; got != want {
		t.Fatalf("first operation value = %q, want %q", got, want)
	}
	if got, want := api.second, "second-message"; got != want {
		t.Fatalf("second operation value = %q, want %q", got, want)
	}
}

type compiledBindingDuplicateTopicAPI struct{}

func (*compiledBindingDuplicateTopicAPI) First(compiledBindingJSONMessage)  {}
func (*compiledBindingDuplicateTopicAPI) Second(compiledBindingJSONMessage) {}

func (*compiledBindingDuplicateTopicAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"First":  {Transport: fb.KafkaConsumer("koba.test.duplicate")},
		"Second": {Transport: fb.KafkaConsumer("koba.test.duplicate")},
	}}
}

func TestBinderBindAPIRejectsDuplicateTopicWithoutPartialRegistration(t *testing.T) {
	binder := newCompiledBindingBinder(t, "koba.test.duplicate")
	err := binder.BindAPI(&compiledBindingDuplicateTopicAPI{})
	if err == nil || !strings.Contains(err.Error(), "declared by more than one operation") {
		t.Fatalf("BindAPI() error = %v, want duplicate topic error", err)
	}
	if binder.Handlers != nil {
		t.Fatalf("handlers = %#v, want no partial registration", binder.Handlers)
	}
}

func TestBinderBindAPIRejectsPreviouslyRegisteredTopic(t *testing.T) {
	existing := func(context.Context, *kafka.Message) error { return nil }
	binder := Binder{Handlers: map[string]Handler{compiledBindingJSONTopic: existing}}
	err := binder.BindAPI(&compiledBindingJSONAPI{})
	if err == nil || !strings.Contains(err.Error(), "already has a handler") {
		t.Fatalf("BindAPI() error = %v, want existing handler error", err)
	}
	if got := reflect.ValueOf(binder.Handlers[compiledBindingJSONTopic]).Pointer(); got != reflect.ValueOf(existing).Pointer() {
		t.Fatal("BindAPI() replaced the existing handler")
	}
}

func TestBinderBindAPIRequiresExactSingleTopicReaderWithConsumerGroup(t *testing.T) {
	reader := func(config kafka.ReaderConfig) *kafka.Reader {
		value := kafka.NewReader(config)
		t.Cleanup(func() { _ = value.Close() })
		return value
	}
	tests := []struct {
		name   string
		binder Binder
		want   string
	}{
		{name: "missing reader", want: "has no configured reader"},
		{
			name:   "nil reader",
			binder: Binder{Readers: map[string]*kafka.Reader{compiledBindingJSONTopic: nil}},
			want:   "has a nil reader",
		},
		{
			name: "different topic",
			binder: Binder{Readers: map[string]*kafka.Reader{
				compiledBindingJSONTopic: reader(kafka.ReaderConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "different.topic",
					GroupID: "koba-test",
				}),
			}},
			want: `configured for "different.topic"`,
		},
		{
			name: "multiple topics",
			binder: Binder{Readers: map[string]*kafka.Reader{
				compiledBindingJSONTopic: reader(kafka.ReaderConfig{
					Brokers:     []string{"localhost:9092"},
					GroupID:     "koba-test",
					GroupTopics: []string{compiledBindingJSONTopic, "other.topic"},
				}),
			}},
			want: "GroupTopics is not supported",
		},
		{
			name: "missing consumer group",
			binder: Binder{Readers: map[string]*kafka.Reader{
				compiledBindingJSONTopic: reader(kafka.ReaderConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   compiledBindingJSONTopic,
				}),
			}},
			want: "requires GroupID",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.binder.BindAPI(&compiledBindingJSONAPI{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BindAPI() error = %v, want it to contain %q", err, test.want)
			}
			if test.binder.Handlers != nil {
				t.Fatalf("handlers = %#v, want no partial registration", test.binder.Handlers)
			}
		})
	}
}

type compiledBindingHTTPOnlyAPI struct{}

func (*compiledBindingHTTPOnlyAPI) Health() string { return "ok" }

func TestBinderBindAPIRejectsAPIsWithoutKafkaConsumers(t *testing.T) {
	var binder Binder
	err := binder.BindAPI(&compiledBindingHTTPOnlyAPI{})
	if err == nil || !strings.Contains(err.Error(), "declares no Kafka consumer operations") {
		t.Fatalf("BindAPI() error = %v, want no Kafka consumers error", err)
	}
	if binder.Handlers != nil {
		t.Fatalf("handlers = %#v, want none", binder.Handlers)
	}
}

func TestBinderBindAPIRejectsNilBinder(t *testing.T) {
	var binder *Binder
	err := binder.BindAPI(&compiledBindingJSONAPI{})
	if err == nil || !strings.Contains(err.Error(), "binder is nil") {
		t.Fatalf("BindAPI() error = %v, want nil binder error", err)
	}
}

func TestBinderBindAPIRejectsNilAPIReceiver(t *testing.T) {
	var api *compiledBindingJSONAPI
	var binder Binder
	err := binder.BindAPI(api)
	if err == nil || !strings.Contains(err.Error(), "receiver must be non-nil") {
		t.Fatalf("BindAPI() error = %v, want nil receiver error", err)
	}
}

type compiledBindingUnexpectedResultAPI struct{}

func (*compiledBindingUnexpectedResultAPI) KobaCompiledAPI() fb.CompiledAPI {
	return fb.CompiledAPI{
		Description: fb.APIDescription{Operations: []fb.OperationDescription{{
			MethodName:  "Consume",
			OperationID: "consumeUnexpectedResult",
			InputType:   reflect.TypeFor[compiledBindingJSONMessage](),
			Contract: fb.OperationContract{
				Transport: fb.KafkaConsumer("koba.test.unexpected-result"),
			},
		}}},
		Invokers: map[string]fb.OperationInvoker{
			"Consume": func(invocation fb.Invocation) (fb.InvocationResult, error) {
				var message compiledBindingJSONMessage
				if err := invocation.Decode(&message); err != nil {
					return fb.InvocationResult{}, err
				}
				return fb.InvocationResult{Value: message, HasValue: true}, nil
			},
		},
	}
}

func TestBinderBindAPIRejectsUnexpectedInvokerResult(t *testing.T) {
	binder := newCompiledBindingBinder(t, "koba.test.unexpected-result")
	if err := binder.BindAPI(&compiledBindingUnexpectedResultAPI{}); err != nil {
		t.Fatalf("BindAPI() error = %v", err)
	}

	err := binder.Handlers["koba.test.unexpected-result"](
		context.Background(),
		&kafka.Message{Value: []byte(`{"id":"item-123"}`)},
	)
	if err == nil || !strings.Contains(err.Error(), "returned an unexpected value") {
		t.Fatalf("handler() error = %v, want unexpected value error", err)
	}
}
