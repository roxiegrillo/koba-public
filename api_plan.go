package koba

import (
	"context"
	"fmt"
	"reflect"
)

// BodyDecoder decodes the transport request body into target. Keeping the
// decoder on Invocation lets generated invokers remain independent of Gin.
type BodyDecoder func(target any) error

// Invocation contains the transport-neutral inputs made available to an API
// operation. Context carries normalized OperationInputs and, for compatibility,
// the active adapter may also attach its legacy transport values.
type Invocation struct {
	Context    context.Context
	DecodeBody BodyDecoder
	// Input is a request value already constructed and validated by the active
	// transport. HasInput distinguishes an intentional nil/zero request from the
	// legacy path where the invoker asks DecodeBody to populate it.
	Input    any
	HasInput bool
}

// Decode invokes the transport body decoder and provides the same explicit
// error in reflected and generated plans when a caller omitted that decoder.
func (invocation Invocation) Decode(target any) error {
	if invocation.HasInput {
		return assignInvocationInput(target, invocation.Input)
	}
	if invocation.DecodeBody == nil {
		return fmt.Errorf("koba: body decoder is not available")
	}
	return invocation.DecodeBody(target)
}

func assignInvocationInput(target any, input any) error {
	destination := reflect.ValueOf(target)
	if !destination.IsValid() || destination.Kind() != reflect.Pointer || destination.IsNil() {
		return fmt.Errorf("koba: invocation input target must be a non-nil pointer")
	}
	destination = destination.Elem()
	source := reflect.ValueOf(input)
	if !source.IsValid() {
		return fmt.Errorf("koba: constructed invocation input is nil")
	}
	if source.Type().AssignableTo(destination.Type()) {
		destination.Set(source)
		return nil
	}
	if source.Kind() == reflect.Pointer && !source.IsNil() && source.Elem().Type().AssignableTo(destination.Type()) {
		destination.Set(source.Elem())
		return nil
	}
	return fmt.Errorf(
		"koba: constructed invocation input has type %s, want %s",
		source.Type(),
		destination.Type(),
	)
}

// InvocationResult distinguishes a void operation from an operation which
// intentionally returned a value (including a nil value).
type InvocationResult struct {
	Value    any
	HasValue bool
}

// OperationInvoker invokes one Koba API method. Reflection-backed and
// generated implementations both satisfy this contract.
type OperationInvoker func(Invocation) (InvocationResult, error)

// CompiledAPI is the normalized execution plan consumed by transports. The
// Description is also the single input used by OpenAPI renderers. Invokers are
// keyed by the exported Go method name in Description.Operations.
type CompiledAPI struct {
	Description APIDescription
	Invokers    map[string]OperationInvoker
}

// WithCompiledAPI is implemented by generated code when a Koba API has
// a static execution plan. APIs which do not implement it continue to use the
// reflection compiler.
type WithCompiledAPI interface {
	KobaCompiledAPI() CompiledAPI
}

// CompileAPI resolves an API to the common execution plan. Generated plans
// are preferred when present; reflection remains the complete fallback.
func CompileAPI(api any) (compiled CompiledAPI, err error) {
	if api == nil || reflect.TypeOf(api) == nil {
		return CompiledAPI{}, fmt.Errorf("koba: API value is nil; pass an API value or typed nil pointer")
	}
	if generated, ok := api.(WithCompiledAPI); ok {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("koba: generated API plan panicked: %v", recovered)
			}
		}()
		compiled = generated.KobaCompiledAPI()
		if err := validateCompiledAPI(&compiled); err != nil {
			return CompiledAPI{}, err
		}
		return compiled, nil
	}
	return compileReflectedAPI(api)
}

func compileReflectedAPI(api any) (CompiledAPI, error) {
	description, err := describeReflectedAPI(api)
	if err != nil {
		return CompiledAPI{}, err
	}
	apiType := reflect.TypeOf(api)
	apiValue := reflect.ValueOf(api)
	compiled := CompiledAPI{
		Description: description,
		Invokers:    make(map[string]OperationInvoker, len(description.Operations)),
	}
	for _, operation := range description.Operations {
		method, ok := apiType.MethodByName(operation.MethodName)
		if !ok {
			return CompiledAPI{}, fmt.Errorf("koba: reflected API method disappeared: %s", operation.MethodName)
		}
		methodValue := apiValue.MethodByName(operation.MethodName)
		inputTypes, _ := GetInputOutputTypes(method)
		compiled.Invokers[operation.MethodName] = reflectedOperationInvoker(inputTypes, methodValue)
	}
	return compiled, nil
}

func reflectedOperationInvoker(inputTypes []reflect.Type, method reflect.Value) OperationInvoker {
	hasContext := HasContext(inputTypes)
	return func(invocation Invocation) (InvocationResult, error) {
		inputs := make([]reflect.Value, 0, len(inputTypes))
		if hasContext {
			inputs = append(inputs, reflect.ValueOf(invocation.Context))
		}
		if originalType := bodyInputType(inputTypes); originalType != nil {
			if originalType.Kind() == reflect.Pointer {
				var value reflect.Value
				if invocation.HasInput {
					value = reflect.New(originalType).Elem()
					if err := invocation.Decode(value.Addr().Interface()); err != nil {
						return InvocationResult{}, err
					}
				} else {
					value = reflect.New(originalType.Elem())
					if err := invocation.Decode(value.Interface()); err != nil {
						return InvocationResult{}, err
					}
				}
				inputs = append(inputs, value)
			} else {
				value := reflect.New(originalType)
				if err := invocation.Decode(value.Interface()); err != nil {
					return InvocationResult{}, err
				}
				inputs = append(inputs, value.Elem())
			}
		}

		outputs := method.Call(inputs)
		if len(outputs) == 0 {
			return InvocationResult{}, nil
		}
		output := outputs[0]
		if output.Kind() == reflect.Pointer {
			if output.IsNil() {
				return InvocationResult{Value: nil, HasValue: true}, nil
			}
			output = output.Elem()
		}
		return InvocationResult{Value: output.Interface(), HasValue: true}, nil
	}
}

func validateCompiledAPI(compiled *CompiledAPI) error {
	if len(compiled.Description.Operations) == 0 {
		return fmt.Errorf("koba: compiled API contains no operations")
	}
	known := make(map[string]struct{}, len(compiled.Description.Operations))
	for index := range compiled.Description.Operations {
		operation := &compiled.Description.Operations[index]
		if operation.Contract.Request != nil {
			return fmt.Errorf(
				"koba: %s: compiled plans must contain resolved RequestBindings instead of a request companion",
				operation.MethodName,
			)
		}
		normalized, err := normalizeOperationContract(operation.Contract)
		if err != nil {
			return fmt.Errorf("koba: %s: %w", operation.MethodName, err)
		}
		operation.Contract = normalized
		requestBindings, err := normalizeResolvedRequestBindings(operation.RequestBindings, operation.InputType)
		if err != nil {
			return fmt.Errorf("koba: %s: %w", operation.MethodName, err)
		}
		operation.RequestBindings = requestBindings
		if _, duplicate := known[operation.MethodName]; duplicate {
			return fmt.Errorf("koba: compiled API contains duplicate method %q", operation.MethodName)
		}
		known[operation.MethodName] = struct{}{}
		if compiled.Invokers[operation.MethodName] == nil {
			return fmt.Errorf("koba: compiled API has no invoker for %q", operation.MethodName)
		}
	}
	for method, invoker := range compiled.Invokers {
		if _, exists := known[method]; !exists {
			return fmt.Errorf("koba: compiled API has an invoker for unknown method %q", method)
		}
		if invoker == nil {
			return fmt.Errorf("koba: compiled API has a nil invoker for %q", method)
		}
	}
	return ValidateAPIDescription(compiled.Description)
}
