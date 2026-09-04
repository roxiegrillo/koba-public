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

func isPost(inputTypes []reflect.Type, outputTypes []reflect.Type) (bool, error) {
	hasCtx := HasContext(inputTypes)
	numIn := len(inputTypes)
	numOut := len(outputTypes)
	base := 0
	if hasCtx {
		base = 1
	}
	if numIn == (base+1) && (numOut == 0 || numOut == 1) {
		return true, nil
	} else if numIn == (base) && (numOut == 0 || numOut == 1) {
		return false, nil
	} else {
		return false, stderr.New("Invalid args")
	}
}
