package workers

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

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
