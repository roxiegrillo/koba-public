package openapi

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestProtobufMessageSchemaMatchesProtoJSONForInt64TimestampAndRepeatedMessages(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("fixture/socket.proto"),
		Package:    proto.String("fixture.socket"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Message"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("sequence"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:     proto.String("created_at"),
						JsonName: proto.String("createdAt"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Timestamp"),
					},
				},
			},
			{
				Name: proto.String("Batch"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:     proto.String("messages"),
					Number:   proto.Int32(1),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
					TypeName: proto.String(".fixture.socket.Message"),
				}},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("construct protobuf descriptor: %v", err)
	}
	schemas := make(openapi3.Schemas)
	batch := file.Messages().ByName("Batch")
	schema, err := protobufMessageSchema(schemas, batch)
	if err != nil {
		t.Fatalf("protobufMessageSchema() returned an error: %v", err)
	}
	messages := schema.Properties["messages"]
	if messages == nil || messages.Value == nil || messages.Value.Items == nil || messages.Value.Items.Ref == "" {
		t.Fatalf("repeated message schema = %#v, want array with component ref", messages)
	}
	messageComponent := schemas[protobufComponentName("fixture.socket.Message")]
	if messageComponent == nil || messageComponent.Value == nil {
		t.Fatalf("message component is missing: %#v", schemas)
	}
	sequence := messageComponent.Value.Properties["sequence"]
	if sequence == nil || sequence.Value == nil || sequence.Value.Type == nil || !sequence.Value.Type.Is("string") || sequence.Value.Pattern != `^-?[0-9]+$` {
		t.Errorf("int64 protojson schema = %#v, want numeric string", sequence)
	}
	createdAt := messageComponent.Value.Properties["created_at"]
	if createdAt == nil || createdAt.Value == nil || createdAt.Value.Type == nil || !createdAt.Value.Type.Is("string") || createdAt.Value.Format != "date-time" {
		t.Errorf("Timestamp protojson schema = %#v, want date-time string", createdAt)
	}

	// The constructed graph remains a valid OpenAPI component graph after refs
	// are attached, including the recursive registration path used in builds.
	document := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "fixture", Version: "1"},
		Paths:   openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: schemas,
		},
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("protobuf component graph validation: %v", err)
	}
}
