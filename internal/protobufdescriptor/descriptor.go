// Package protobufdescriptor derives deterministic contract metadata from a
// generated protobuf message type.
package protobufdescriptor

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"sort"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Description contains the portable protobuf metadata used by contract
// projections and generated clients.
type Description struct {
	FullName            string
	Base64DescriptorSet string
}

// SetDescription contains the portable protobuf metadata shared by one or
// more directional messages. FullNames preserves the input order while the
// descriptor files are de-duplicated and serialized in stable path order.
type SetDescription struct {
	FullNames           []string
	Base64DescriptorSet string
}

// MessageDescriptor returns the reflective message descriptor for a generated
// protobuf Go type after applying the same validation used by DescribeSet.
func MessageDescriptor(messageType reflect.Type) (protoreflect.MessageDescriptor, error) {
	message, err := newMessage(messageType)
	if err != nil {
		return nil, err
	}
	descriptor := message.ProtoReflect().Descriptor()
	if descriptor == nil || descriptor.ParentFile() == nil {
		return nil, fmt.Errorf("protobuf message type %s has no descriptor", messageType)
	}
	return descriptor, nil
}

// Describe validates messageType and serializes its complete transitive file
// descriptor graph in stable path order.
func Describe(messageType reflect.Type) (Description, error) {
	set, err := DescribeSet(messageType)
	if err != nil {
		return Description{}, err
	}
	return Description{FullName: set.FullNames[0], Base64DescriptorSet: set.Base64DescriptorSet}, nil
}

// DescribeSet validates every message type and serializes the union of their
// complete transitive file descriptor graphs in stable path order.
func DescribeSet(messageTypes ...reflect.Type) (SetDescription, error) {
	if len(messageTypes) == 0 {
		return SetDescription{}, fmt.Errorf("at least one protobuf message Go type is required")
	}

	files := make(map[string]*descriptorpb.FileDescriptorProto)
	fullNames := make([]string, 0, len(messageTypes))
	for _, messageType := range messageTypes {
		descriptor, err := MessageDescriptor(messageType)
		if err != nil {
			return SetDescription{}, err
		}
		fullNames = append(fullNames, string(descriptor.FullName()))
		collectFile(descriptor.ParentFile(), files)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	set := &descriptorpb.FileDescriptorSet{File: make([]*descriptorpb.FileDescriptorProto, 0, len(paths))}
	for _, path := range paths {
		set.File = append(set.File, files[path])
	}
	serialized, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		return SetDescription{}, fmt.Errorf("serialize protobuf descriptor set: %w", err)
	}
	return SetDescription{
		FullNames:           fullNames,
		Base64DescriptorSet: base64.StdEncoding.EncodeToString(serialized),
	}, nil
}

func newMessage(messageType reflect.Type) (proto.Message, error) {
	if messageType == nil {
		return nil, fmt.Errorf("protobuf message Go type is required")
	}
	if messageType.Kind() != reflect.Pointer || messageType.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("protobuf message Go type must be a pointer to a struct, got %s", messageType)
	}
	value := reflect.New(messageType.Elem()).Interface()
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("protobuf message Go type %s does not implement proto.Message", messageType)
	}
	return message, nil
}

func collectFile(file protoreflect.FileDescriptor, files map[string]*descriptorpb.FileDescriptorProto) {
	if file == nil {
		return
	}
	path := file.Path()
	if _, exists := files[path]; exists {
		return
	}
	files[path] = protodesc.ToFileDescriptorProto(file)
	imports := file.Imports()
	for index := 0; index < imports.Len(); index++ {
		collectFile(imports.Get(index).FileDescriptor, files)
	}
}
