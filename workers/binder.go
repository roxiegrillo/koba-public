package workers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kenshaw/snaker"
	koba "github.com/roxiegrillo/koba/v2"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

type (
	Handler func(ctx context.Context, msg *kafka.Message) error
	Binder  struct {
		Readers  map[string]*kafka.Reader
		Handlers map[string]Handler
	}
)

var pattern *regexp.Regexp

func init() {
	p, err := regexp.Compile(".*[.](.*)")
	pattern = p

	if err != nil {
		panic(err)
	}
}

func (b *Binder) Bind(api interface{}) {
	namespace := reflect.TypeOf(api).String()

	subMatches := pattern.FindStringSubmatch(namespace)
	if len(subMatches) > 0 {
		namespace = subMatches[1]
	}

	methodValuePairs := koba.GetMethodValueGenericTypes(api)

	for _, p := range methodValuePairs {
		method := p.Left
		value := p.Right
		inputTypes, _ := koba.GetInputOutputTypes(method)
		hasCtx := koba.HasContext(inputTypes)

		topicName := strings.ToUpper(fmt.Sprintf("%s-%s", snaker.CamelToSnake(namespace), snaker.CamelToSnake(method.Name)))

		b.Handlers[topicName] = func(ctx context.Context, kafkaMessage *kafka.Message) error {
			logger := zerolog.Ctx(ctx).With().Str("topic", topicName).Logger()

			ctx = logger.WithContext(ctx)

			input, err := computeInput(ctx, inputTypes, hasCtx, kafkaMessage.Value)
			if err != nil {
				return err
			}

			values, iErr := koba.UnBindError(func() []reflect.Value {
				return value.Call(input)
			})
			if iErr.Error != nil {
				if e, ok := iErr.Error.(error); ok {
					return e
				}
				return errors.New(fmt.Sprint(iErr.Error))
			} else {
				if len(values) > 0 {
					logger.Info().Msg("ignoring return value")
				} else {
					logger.Info().Msg("nothing to return")
				}
			}

			return nil
		}

	}
}

// Validate checks the reader configuration required for explicit offset
// commits before any worker is started.
func (b *Binder) Validate() error {
	if b == nil {
		return errors.New("koba/workers: binder is nil")
	}
	for topic, handler := range b.Handlers {
		reader, configured := b.Readers[topic]
		if !configured {
			continue
		}
		if handler == nil {
			return fmt.Errorf("koba/workers: topic %q has a nil handler", topic)
		}
		if reader == nil {
			return fmt.Errorf("koba/workers: topic %q has a nil reader", topic)
		}
		if strings.TrimSpace(reader.Config().GroupID) == "" {
			return fmt.Errorf(
				"koba/workers: topic %q reader requires GroupID for explicit offset commits",
				topic,
			)
		}
	}
	return nil
}

// Run starts every configured worker and blocks until ctx is cancelled and all
// workers have stopped. Invalid commit configuration fails fast.
func (b *Binder) Run(ctx context.Context) {
	if ctx == nil {
		panic("koba/workers: context is nil")
	}
	if err := b.Validate(); err != nil {
		panic(err)
	}

	var workers sync.WaitGroup

	for topic, handler := range b.Handlers {
		if reader, ok := b.Readers[topic]; ok {
			worker := RunReaderWorker{
				Reader:  reader,
				Handler: handler,
				Topic:   topic,
			}

			workers.Add(1)
			go func(worker RunReaderWorker) {
				defer workers.Done()
				worker.run(ctx)
			}(worker)
		}
	}

	<-ctx.Done()
	workers.Wait()
}

type RunReaderWorker struct {
	Reader  MessageReader
	Handler Handler
	Topic   string
	// RetryDelay is applied after fetch, handler, and commit failures. Values
	// less than or equal to zero use the one-second default.
	RetryDelay time.Duration
}

// MessageReader is the subset of kafka.Reader used by a worker. FetchMessage
// deliberately does not commit offsets; the worker commits only after the
// handler completes successfully.
type MessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
}

const defaultRetryDelay = time.Second

// Work starts consuming messages in the background. Cancellation is
// controlled by ctx; Binder.Run additionally waits for all workers to stop.
func (w *RunReaderWorker) Work(ctx context.Context) {
	go w.run(ctx)
}

func (w *RunReaderWorker) run(ctx context.Context) {
	baseLogger := zerolog.Ctx(ctx).With().Str("topic", w.Topic).Logger()

	baseLogger.Info().Msg("starting reader worker")

	for {
		msg, err := w.Reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			baseLogger.Error().Err(err).Msg("fetching kafka message")
			if !w.waitForRetry(ctx) {
				return
			}
			continue
		}

		logger := baseLogger.With().Str("key", string(msg.Key)).Logger()
		for {
			if err := w.Handler(ctx, &msg); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error().Err(err).Msg("executing kafka handler; message will be retried")
				if !w.waitForRetry(ctx) {
					return
				}
				continue
			}
			break
		}

		// Handling and committing are separate retry phases. Once the handler
		// succeeds, a transient commit failure must not execute it again and
		// duplicate its side effects.
		for {
			if err := w.Reader.CommitMessages(ctx, msg); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error().Err(err).Msg("committing kafka message; commit will be retried")
				if !w.waitForRetry(ctx) {
					return
				}
				continue
			}
			break
		}
	}
}

func (w *RunReaderWorker) waitForRetry(ctx context.Context) bool {
	delay := w.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func computeInput(ctx context.Context, inputType []reflect.Type, hasContext bool, data []byte) ([]reflect.Value, error) {
	if inputType == nil || len(inputType) == 0 {
		logger := zerolog.Ctx(ctx)
		logger.Debug().Msg("no input types provided")
		return []reflect.Value{}, nil
	}

	var input []reflect.Value

	if hasContext {
		input = append(input, reflect.ValueOf(ctx))
	}

	if !hasContext || len(inputType) > 1 {
		idx := len(inputType) - 1

		t := inputType[idx]

		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		request := reflect.New(t)

		err := proto.Unmarshal(data, request.Interface().(proto.Message))
		if err != nil {
			return nil, err
		}

		var val reflect.Value
		if inputType[idx].Kind() == reflect.Ptr {
			val = request
		} else {
			val = reflect.ValueOf(request).Elem()
		}

		input = append(input, val)
	}

	return input, nil
}
