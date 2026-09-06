package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

// BindAPI binds the Kafka consumer operations declared by api to the same
// compiled function plan used by HTTP and WebSocket adapters. Readers remain
// runtime configuration keyed by the explicit contract topic.
func (b *Binder) BindAPI(api any) error {
	if b == nil {
		return errors.New("koba/workers: binder is nil")
	}
	apiValue := reflect.ValueOf(api)
	if !apiValue.IsValid() || (apiValue.Kind() == reflect.Pointer && apiValue.IsNil()) {
		return errors.New("koba/workers: Kafka API receiver must be non-nil")
	}
	compiled, err := fb.CompileAPI(api)
	if err != nil {
		return fmt.Errorf("koba/workers: compile Kafka API: %w", err)
	}

	handlers := make(map[string]Handler)
	for _, operation := range compiled.Description.Operations {
		if !operation.HasKafkaConsumerTransport() {
			continue
		}
		topic := operation.Contract.Transport.KafkaConsumer.Topic
		if _, duplicate := handlers[topic]; duplicate {
			return fmt.Errorf("koba/workers: Kafka topic %q is declared by more than one operation", topic)
		}
		if b.Handlers != nil {
			if _, exists := b.Handlers[topic]; exists {
				return fmt.Errorf("koba/workers: Kafka topic %q already has a handler", topic)
			}
		}
		if err := b.validateAPIReader(topic); err != nil {
			return err
		}
		encoding, err := operation.ResolvedKafkaMessageEncoding()
		if err != nil {
			return fmt.Errorf("koba/workers: %s: %w", operation.MethodName, err)
		}
		invoker := compiled.Invokers[operation.MethodName]
		methodName := operation.MethodName
		handlers[topic] = func(ctx context.Context, message *kafka.Message) error {
			if message == nil {
				return fmt.Errorf("koba/workers: %s received a nil Kafka message", methodName)
			}
			logger := zerolog.Ctx(ctx).With().Str("topic", topic).Str("operation", operation.OperationID).Logger()
			ctx = logger.WithContext(ctx)
			result, err := invokeCompiledKafkaOperation(invoker, fb.Invocation{
				Context:    ctx,
				DecodeBody: kafkaMessageDecoder(encoding, message.Value),
			})
			if err != nil {
				return err
			}
			if result.HasValue {
				return fmt.Errorf("koba/workers: Kafka consumer %s returned an unexpected value", methodName)
			}
			return nil
		}
	}
	if len(handlers) == 0 {
		return errors.New("koba/workers: API declares no Kafka consumer operations")
	}
	if b.Handlers == nil {
		b.Handlers = make(map[string]Handler, len(handlers))
	}
	for topic, handler := range handlers {
		b.Handlers[topic] = handler
	}
	return nil
}

func (b *Binder) validateAPIReader(topic string) error {
	reader, exists := b.Readers[topic]
	if !exists {
		return fmt.Errorf("koba/workers: Kafka consumer topic %q has no configured reader", topic)
	}
	if reader == nil {
		return fmt.Errorf("koba/workers: Kafka consumer topic %q has a nil reader", topic)
	}
	config := reader.Config()
	if len(config.GroupTopics) != 0 {
		return fmt.Errorf(
			"koba/workers: Kafka consumer topic %q requires a single-topic reader; GroupTopics is not supported",
			topic,
		)
	}
	if config.Topic != topic {
		return fmt.Errorf(
			"koba/workers: Kafka consumer topic %q uses a reader configured for %q",
			topic,
			config.Topic,
		)
	}
	if strings.TrimSpace(config.GroupID) == "" {
		return fmt.Errorf(
			"koba/workers: Kafka consumer topic %q reader requires GroupID for explicit offset commits",
			topic,
		)
	}
	return nil
}

func kafkaMessageDecoder(encoding fb.KafkaMessageEncoding, payload []byte) fb.BodyDecoder {
	return func(target any) error {
		switch encoding {
		case fb.KafkaMessageEncodingProtobuf:
			message, ok := target.(proto.Message)
			if !ok {
				return fmt.Errorf("koba/workers: protobuf target has type %T", target)
			}
			return proto.Unmarshal(payload, message)
		case fb.KafkaMessageEncodingJSON:
			return json.Unmarshal(payload, target)
		default:
			return fmt.Errorf("koba/workers: unsupported Kafka message encoding %q", encoding)
		}
	}
}

func invokeCompiledKafkaOperation(
	invoker fb.OperationInvoker,
	invocation fb.Invocation,
) (result fb.InvocationResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if failure, ok := recovered.(error); ok {
				err = failure
			} else {
				err = errors.New(fmt.Sprint(recovered))
			}
			result = fb.InvocationResult{}
		}
	}()
	return invoker(invocation)
}
