package koba

import (
	"context"
	stderr "errors"
	"reflect"
)

func isNotExportable(method reflect.Method) bool {
	if method.Name == "Routes" {
		return true
	}
	if method.Name == "APIDocumentation" &&
		method.Type.NumIn() == 1 &&
		method.Type.NumOut() == 1 &&
		method.Type.Out(0) == reflect.TypeFor[APIDocumentation]() {
		return true
	}
	if method.Name == "APIContract" &&
		method.Type.NumIn() == 1 &&
		method.Type.NumOut() == 1 &&
		method.Type.Out(0) == reflect.TypeFor[APIContract]() {
		return true
	}
	return method.Name == "KobaCompiledAPI" &&
		method.Type.NumIn() == 1 &&
		method.Type.NumOut() == 1 &&
		method.Type.Out(0) == reflect.TypeFor[CompiledAPI]()
}

type Pair[l any, r any] struct {
	Left  l
	Right r
}

func GetMethodValueGenericTypes(api interface{}) (result []*Pair[reflect.Method, reflect.Value]) {
	iType := reflect.TypeOf(api)
	for i := 0; i < iType.NumMethod(); i++ {
		method := iType.Method(i)
		// do not register private functions
		if !method.IsExported() || isNotExportable(method) {
			continue
		}
		methodValue := reflect.ValueOf(api).Method(i)
		result = append(result, &Pair[reflect.Method, reflect.Value]{
			Left:  method,
			Right: methodValue,
		})
	}
	return
}

func GetInputOutputTypes(method reflect.Method) ([]reflect.Type, []reflect.Type) {
	numIn := method.Type.NumIn()
	numOut := method.Type.NumOut()

	var inputType []reflect.Type

	for i := 1; i < numIn; i++ {
		inputType = append(inputType, method.Type.In(i))
	}

	var outputType []reflect.Type

	for i := 0; i < numOut; i++ {
		outputType = append(outputType, method.Type.Out(i))
	}

	return inputType, outputType
}

func HasContext(types []reflect.Type) bool {
	if len(types) == 0 {
		return false
	}
	lastType := types[0]
	request := reflect.New(lastType).Interface()
	_, ok := request.(*context.Context)

	return ok
}

type operationResultSignature struct {
	OutputType   reflect.Type
	ReturnsError bool
}

var errorType = reflect.TypeFor[error]()

func analyzeOperationResults(outputTypes []reflect.Type) (operationResultSignature, error) {
	switch len(outputTypes) {
	case 0:
		return operationResultSignature{}, nil
	case 1:
		if outputTypes[0] == errorType {
			return operationResultSignature{ReturnsError: true}, nil
		}
		return operationResultSignature{OutputType: outputTypes[0]}, nil
	case 2:
		if outputTypes[0] == errorType {
			return operationResultSignature{}, stderr.New("the response value cannot be error when another result follows")
		}
		if outputTypes[1] != errorType {
			return operationResultSignature{}, stderr.New("the second result must be error")
		}
		return operationResultSignature{OutputType: outputTypes[0], ReturnsError: true}, nil
	default:
		return operationResultSignature{}, stderr.New("an operation may return only a response value followed by an optional error")
	}
}

func isPost(inputTypes []reflect.Type) (bool, error) {
	hasCtx := HasContext(inputTypes)
	numIn := len(inputTypes)
	base := 0
	if hasCtx {
		base = 1
	}
	if numIn == base+1 {
		return true, nil
	} else if numIn == base {
		return false, nil
	} else {
		return false, stderr.New("an operation accepts at most one request value after an optional context.Context")
	}
}
