package workers

import (
	"context"
	"reflect"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

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
			val = request.Elem()
		}

		input = append(input, val)
	}

	return input, nil
}
