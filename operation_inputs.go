package koba

import (
	"context"
	"fmt"
)

type operationInputValue struct {
	value    any
	provided bool
}

// OperationInputs is the transport-neutral set of normalized inputs for one
// invocation. Values include defaults; WasProvided distinguishes them from
// values supplied by the caller.
type OperationInputs struct {
	values map[string]operationInputValue
}

// Value returns a normalized input value by semantic identifier.
func (inputs OperationInputs) Value(identifier string) (any, bool) {
	value, exists := inputs.values[identifier]
	if !exists {
		return nil, false
	}
	return value.value, true
}

// WasProvided reports whether the caller supplied the input rather than the
// framework applying its default.
func (inputs OperationInputs) WasProvided(identifier string) bool {
	value, exists := inputs.values[identifier]
	return exists && value.provided
}

type operationInputsContextKey struct{}

// ContextWithOperationInputs attaches normalized inputs to an invocation
// context. Transport adapters call this after binding and validation.
func ContextWithOperationInputs(ctx context.Context, inputs OperationInputs) context.Context {
	return context.WithValue(ctx, operationInputsContextKey{}, inputs)
}

// InputsFromContext returns the operation inputs attached by the active
// transport. A missing value produces an empty set.
func InputsFromContext(ctx context.Context) OperationInputs {
	if ctx == nil {
		return OperationInputs{}
	}
	inputs, _ := ctx.Value(operationInputsContextKey{}).(OperationInputs)
	return inputs
}

// InputValue returns a typed operation input by semantic identifier. The
// boolean is false when the input is absent. A type mismatch is a programming
// error: transports have already normalized request values against the
// operation contract before invoking the API function.
func InputValue[T any](ctx context.Context, identifier string) (T, bool) {
	var zero T
	value, exists := InputsFromContext(ctx).Value(identifier)
	if !exists {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		panic(fmt.Sprintf(
			"koba: operation input %q has type %T, requested %T",
			identifier,
			value,
			zero,
		))
	}
	return typed, true
}

// InputWasProvided reports whether an input was present on the transport.
func InputWasProvided(ctx context.Context, identifier string) bool {
	return InputsFromContext(ctx).WasProvided(identifier)
}
