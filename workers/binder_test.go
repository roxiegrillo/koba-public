package workers

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (ctx *observedDoneContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func TestBinderRunWaitsForCancellationWithoutSpinning(t *testing.T) {
	baseContext, cancel := context.WithCancel(context.Background())
	ctx := &observedDoneContext{Context: baseContext, observed: make(chan struct{})}
	binder := Binder{
		Readers: map[string]*kafka.Reader{},
		Handlers: map[string]Handler{
			"events": func(context.Context, *kafka.Message) error { return nil },
		},
	}

	// Binder owns concrete kafka readers for compatibility, so exercise its
	// lifecycle with no matching reader: it must block without spinning and
	// return promptly when its context is cancelled.
	done := make(chan struct{})
	go func() {
		defer close(done)
		binder.Run(ctx)
	}()

	select {
	case <-ctx.observed:
	case <-time.After(time.Second):
		t.Fatal("Binder.Run did not reach its cancellation wait")
	}
	select {
	case <-done:
		t.Fatal("Binder.Run returned before cancellation")
	default:
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Binder.Run did not return after cancellation")
	}
}

func TestBinderValidateRequiresConsumerGroup(t *testing.T) {
	withoutGroup := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "events",
	})
	defer withoutGroup.Close()
	withGroup := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "events",
		GroupID: "functional-app-test",
	})
	defer withGroup.Close()

	tests := []struct {
		name    string
		reader  *kafka.Reader
		handler Handler
		want    string
	}{
		{name: "nil handler", reader: withGroup, want: "nil handler"},
		{name: "nil reader", handler: func(context.Context, *kafka.Message) error { return nil }, want: "nil reader"},
		{name: "missing group", reader: withoutGroup, handler: func(context.Context, *kafka.Message) error { return nil }, want: "requires GroupID"},
		{name: "valid group", reader: withGroup, handler: func(context.Context, *kafka.Message) error { return nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binder := Binder{
				Readers:  map[string]*kafka.Reader{"events": test.reader},
				Handlers: map[string]Handler{"events": test.handler},
			}
			err := binder.Validate()
			if test.want == "" && err != nil {
				t.Fatalf("Validate() returned an error: %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}
