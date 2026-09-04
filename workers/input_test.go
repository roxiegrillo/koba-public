package workers

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestComputeInputBuildsContextPointerAndValueArguments(t *testing.T) {
	payload, err := proto.Marshal(wrapperspb.String("item-123"))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	ctx := context.WithValue(context.Background(), binderContextKey{}, "context-123")
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()

	tests := []struct {
		name       string
		inputTypes []reflect.Type
		hasContext bool
		wantCount  int
		assert     func(*testing.T, []reflect.Value)
	}{
		{
			name:      "no arguments",
			wantCount: 0,
		},
		{
			name:       "context only",
			inputTypes: []reflect.Type{contextType},
			hasContext: true,
			wantCount:  1,
			assert: func(t *testing.T, values []reflect.Value) {
				t.Helper()
				if values[0].Interface().(context.Context) != ctx {
					t.Fatal("computed context does not preserve the supplied context")
				}
			},
		},
		{
			name:       "pointer message",
			inputTypes: []reflect.Type{reflect.TypeOf(&wrapperspb.StringValue{})},
			wantCount:  1,
			assert: func(t *testing.T, values []reflect.Value) {
				t.Helper()
				if got, want := values[0].Interface().(*wrapperspb.StringValue).Value, "item-123"; got != want {
					t.Fatalf("pointer message = %q, want %q", got, want)
				}
			},
		},
		{
			name:       "context and value message",
			inputTypes: []reflect.Type{contextType, reflect.TypeOf(wrapperspb.StringValue{})},
			hasContext: true,
			wantCount:  2,
			assert: func(t *testing.T, values []reflect.Value) {
				t.Helper()
				if got, want := values[1].Interface().(wrapperspb.StringValue).Value, "item-123"; got != want {
					t.Fatalf("value message = %q, want %q", got, want)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values, err := computeInput(ctx, test.inputTypes, test.hasContext, payload)
			if err != nil {
				t.Fatalf("computeInput() returned an error: %v", err)
			}
			if got := len(values); got != test.wantCount {
				t.Fatalf("argument count = %d, want %d", got, test.wantCount)
			}
			if test.assert != nil {
				test.assert(t, values)
			}
		})
	}
}

func TestComputeInputRejectsInvalidProtobuf(t *testing.T) {
	_, err := computeInput(
		context.Background(),
		[]reflect.Type{reflect.TypeOf(&wrapperspb.StringValue{})},
		false,
		[]byte{0xff},
	)
	if err == nil {
		t.Fatal("computeInput() accepted an invalid protobuf payload")
	}
}
