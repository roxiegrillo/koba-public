package workers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

type scriptedMessageReader struct {
	message kafka.Message

	mu                sync.Mutex
	fetchErrors       []error
	commitErrors      []error
	messageDelivered  bool
	fetchCalls        int
	commitCalls       int
	fetches           chan int
	commits           chan int
	committedMessages []kafka.Message
}

func newScriptedMessageReader(fetchErrors []error, commitErrors []error) *scriptedMessageReader {
	return &scriptedMessageReader{
		message: kafka.Message{
			Topic: "events", Partition: 3, Offset: 42,
			Key: []byte("link-1"), Value: []byte("event"),
		},
		fetchErrors:  append([]error(nil), fetchErrors...),
		commitErrors: append([]error(nil), commitErrors...),
		fetches:      make(chan int, 16),
		commits:      make(chan int, 16),
	}
}

func (r *scriptedMessageReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	r.mu.Lock()
	r.fetchCalls++
	call := r.fetchCalls
	if len(r.fetchErrors) != 0 {
		err := r.fetchErrors[0]
		r.fetchErrors = r.fetchErrors[1:]
		r.mu.Unlock()
		r.fetches <- call
		return kafka.Message{}, err
	}
	if !r.messageDelivered {
		r.messageDelivered = true
		message := r.message
		r.mu.Unlock()
		r.fetches <- call
		return message, nil
	}
	r.mu.Unlock()
	r.fetches <- call
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (r *scriptedMessageReader) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	r.mu.Lock()
	r.commitCalls++
	call := r.commitCalls
	r.committedMessages = append(r.committedMessages, messages...)
	var err error
	if len(r.commitErrors) != 0 {
		err = r.commitErrors[0]
		r.commitErrors = r.commitErrors[1:]
	}
	r.mu.Unlock()
	r.commits <- call
	return err
}

func (r *scriptedMessageReader) callCounts() (fetches int, commits int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fetchCalls, r.commitCalls
}

func (r *scriptedMessageReader) committed() []kafka.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]kafka.Message(nil), r.committedMessages...)
}

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (ctx *observedDoneContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func runTestWorker(ctx context.Context, worker *RunReaderWorker) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.run(ctx)
	}()
	return done
}

func waitForCall(t *testing.T, calls <-chan int, want int, name string) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case call := <-calls:
			if call >= want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s call %d", name, want)
		}
	}
}

func stopWorker(t *testing.T, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestRunReaderWorkerCommitsAfterSuccessfulHandling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader(nil, nil)
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Millisecond,
		Handler: func(_ context.Context, message *kafka.Message) error {
			handlerCalls.Add(1)
			if string(message.Key) != "link-1" {
				t.Errorf("handled key = %q, want link-1", message.Key)
			}
			return nil
		},
	}

	done := runTestWorker(ctx, &worker)
	waitForCall(t, reader.commits, 1, "commit")
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	stopWorker(t, cancel, done)
}

func TestRunReaderWorkerRetriesFailedHandlerBeforeCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader(nil, nil)
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Millisecond,
		Handler: func(context.Context, *kafka.Message) error {
			if handlerCalls.Add(1) == 1 {
				return errors.New("database unavailable")
			}
			return nil
		},
	}

	done := runTestWorker(ctx, &worker)
	waitForCall(t, reader.commits, 1, "commit")
	_, commitCalls := reader.callCounts()
	if got := handlerCalls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
	if commitCalls != 1 {
		t.Fatalf("commit calls = %d, want 1", commitCalls)
	}
	stopWorker(t, cancel, done)
}

func TestRunReaderWorkerRetriesCommitWithoutRepeatingHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader(nil, []error{errors.New("coordinator unavailable")})
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Millisecond,
		Handler: func(context.Context, *kafka.Message) error {
			handlerCalls.Add(1)
			return nil
		},
	}

	done := runTestWorker(ctx, &worker)
	waitForCall(t, reader.commits, 2, "commit")
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1 after a commit retry", got)
	}
	committed := reader.committed()
	if len(committed) != 2 {
		t.Fatalf("committed messages = %d, want 2 attempts", len(committed))
	}
	for index, message := range committed {
		if message.Topic != "events" || message.Partition != 3 || message.Offset != 42 {
			t.Fatalf("commit attempt %d = %s/%d/%d, want events/3/42", index+1, message.Topic, message.Partition, message.Offset)
		}
	}
	stopWorker(t, cancel, done)
}

func TestRunReaderWorkerCancellationStopsCommitRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader(nil, []error{errors.New("coordinator unavailable")})
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Hour,
		Handler: func(context.Context, *kafka.Message) error {
			handlerCalls.Add(1)
			return nil
		},
	}

	done := runTestWorker(ctx, &worker)
	waitForCall(t, reader.commits, 1, "commit")
	stopWorker(t, cancel, done)
	fetchCalls, commitCalls := reader.callCounts()
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	if commitCalls != 1 {
		t.Fatalf("commit calls = %d, want 1", commitCalls)
	}
	if fetchCalls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetchCalls)
	}
}

func TestRunReaderWorkerRetriesFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader([]error{errors.New("broker unavailable")}, nil)
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Millisecond,
		Handler: func(context.Context, *kafka.Message) error {
			handlerCalls.Add(1)
			return nil
		},
	}

	done := runTestWorker(ctx, &worker)
	waitForCall(t, reader.commits, 1, "commit")
	fetchCalls, _ := reader.callCounts()
	if fetchCalls < 2 {
		t.Fatalf("fetch calls = %d, want at least 2", fetchCalls)
	}
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	stopWorker(t, cancel, done)
}

func TestRunReaderWorkerCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := newScriptedMessageReader(nil, nil)
	handled := make(chan struct{}, 1)
	var handlerCalls atomic.Int32
	worker := RunReaderWorker{
		Reader:     reader,
		Topic:      "events",
		RetryDelay: time.Hour,
		Handler: func(context.Context, *kafka.Message) error {
			handlerCalls.Add(1)
			handled <- struct{}{}
			return errors.New("database unavailable")
		},
	}

	done := runTestWorker(ctx, &worker)
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("handler was not called")
	}
	stopWorker(t, cancel, done)
	_, commitCalls := reader.callCounts()
	if got := handlerCalls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	if commitCalls != 0 {
		t.Fatalf("commit calls = %d, want 0", commitCalls)
	}
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
		GroupID: "koba-app-test",
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
