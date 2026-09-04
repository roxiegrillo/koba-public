package workers

import (
	"context"
	"strings"
	"testing"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type binderBindingFixtureAPI struct {
	contextValue string
	message      string
}

func (api *binderBindingFixtureAPI) Consume(ctx context.Context, message *wrapperspb.StringValue) {
	api.contextValue, _ = ctx.Value(binderContextKey{}).(string)
	api.message = message.Value
}

type binderContextKey struct{}

func TestBinderBindRegistersAndDecodesHandlerOnZeroValue(t *testing.T) {
	api := &binderBindingFixtureAPI{}
	var binder Binder
	binder.Bind(api)

	const topic = "BINDER_BINDING_FIXTURE_API-CONSUME"
	handler, found := binder.Handlers[topic]
	if !found {
		t.Fatalf("registered topics = %#v, want %q", binder.Handlers, topic)
	}
	payload, err := proto.Marshal(wrapperspb.String("item-123"))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	ctx := context.WithValue(context.Background(), binderContextKey{}, "context-123")
	if err := handler(ctx, &kafka.Message{Value: payload}); err != nil {
		t.Fatalf("handler returned an error: %v", err)
	}
	if got, want := api.message, "item-123"; got != want {
		t.Fatalf("decoded message = %q, want %q", got, want)
	}
	if got, want := api.contextValue, "context-123"; got != want {
		t.Fatalf("context value = %q, want %q", got, want)
	}

	if err := handler(ctx, &kafka.Message{Value: []byte{0xff}}); err == nil {
		t.Fatal("handler accepted an invalid protobuf payload")
	}
}

type binderPanicFixtureAPI struct{}

func (*binderPanicFixtureAPI) Consume(*wrapperspb.StringValue) {
	panic("consumer failed")
}

func TestBinderHandlerConvertsBusinessPanicToError(t *testing.T) {
	binder := Binder{}
	binder.Bind(&binderPanicFixtureAPI{})
	payload, err := proto.Marshal(wrapperspb.String("item-123"))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	const topic = "BINDER_PANIC_FIXTURE_API-CONSUME"
	err = binder.Handlers[topic](context.Background(), &kafka.Message{Value: payload})
	if err == nil || !strings.Contains(err.Error(), "consumer failed") {
		t.Fatalf("handler error = %v, want recovered business panic", err)
	}
}
