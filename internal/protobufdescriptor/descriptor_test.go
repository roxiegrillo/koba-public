package protobufdescriptor

import (
	"encoding/base64"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/typepb"
)

func TestDescribeProducesDeterministicTransitiveDescriptorSet(t *testing.T) {
	first, err := Describe(reflect.TypeFor[*typepb.Type]())
	if err != nil {
		t.Fatalf("Describe() returned an error: %v", err)
	}
	second, err := Describe(reflect.TypeFor[*typepb.Type]())
	if err != nil {
		t.Fatalf("second Describe() returned an error: %v", err)
	}
	if first != second {
		t.Fatalf("Describe() is nondeterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if got, want := first.FullName, "google.protobuf.Type"; got != want {
		t.Fatalf("full name = %q, want %q", got, want)
	}
	serialized, err := base64.StdEncoding.DecodeString(first.Base64DescriptorSet)
	if err != nil {
		t.Fatalf("decode descriptor set: %v", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(serialized, set); err != nil {
		t.Fatalf("unmarshal descriptor set: %v", err)
	}
	wantPaths := map[string]bool{
		"google/protobuf/type.proto":           false,
		"google/protobuf/any.proto":            false,
		"google/protobuf/source_context.proto": false,
	}
	previous := ""
	for _, file := range set.File {
		path := file.GetName()
		if previous != "" && path < previous {
			t.Fatalf("descriptor paths are not sorted: %q before %q", previous, path)
		}
		previous = path
		if _, expected := wantPaths[path]; expected {
			wantPaths[path] = true
		}
	}
	for path, present := range wantPaths {
		if !present {
			t.Errorf("descriptor set is missing %q", path)
		}
	}
}

func TestDescribeSetCombinesDirectionalMessages(t *testing.T) {
	description, err := DescribeSet(
		reflect.TypeFor[*typepb.Type](),
		reflect.TypeFor[*anypb.Any](),
	)
	if err != nil {
		t.Fatalf("DescribeSet() returned an error: %v", err)
	}
	if got, want := description.FullNames, []string{"google.protobuf.Type", "google.protobuf.Any"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("full names = %#v, want %#v", got, want)
	}
	serialized, err := base64.StdEncoding.DecodeString(description.Base64DescriptorSet)
	if err != nil {
		t.Fatalf("decode descriptor set: %v", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(serialized, set); err != nil {
		t.Fatalf("unmarshal descriptor set: %v", err)
	}
	paths := make(map[string]bool, len(set.File))
	for _, file := range set.File {
		paths[file.GetName()] = true
	}
	for _, path := range []string{"google/protobuf/type.proto", "google/protobuf/any.proto"} {
		if !paths[path] {
			t.Errorf("descriptor set is missing %q", path)
		}
	}
}
