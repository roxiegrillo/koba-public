package workers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/kenshaw/snaker"
	koba "github.com/roxiegrillo/koba/v2"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
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
	if b.Handlers == nil {
		b.Handlers = make(map[string]Handler)
	}
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
