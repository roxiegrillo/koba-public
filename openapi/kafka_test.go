package openapi

import (
	"context"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
)

type kafkaOpenAPIEvent struct {
	ID string `json:"id"`
}

type kafkaOpenAPIStatus struct {
	Status string `json:"status"`
}

type mixedKafkaOpenAPI struct{}

func (*mixedKafkaOpenAPI) Consume(_ context.Context, _ kafkaOpenAPIEvent) error { return nil }

func (*mixedKafkaOpenAPI) Status() kafkaOpenAPIStatus {
	return kafkaOpenAPIStatus{Status: "ok"}
}

func (*mixedKafkaOpenAPI) Routes() map[string]string {
	return map[string]string{"Status": "/status"}
}

func (*mixedKafkaOpenAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Consume": {Transport: fb.KafkaConsumer("inventory.items.created")},
	}}
}

func (*mixedKafkaOpenAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{
		Name: "mixed-transport",
		Operations: map[string]fb.OperationDocumentation{
			"Consume": {
				OperationID: "consumeKafkaOpenAPIEvent",
				Summary:     "Consume an item event",
				Tags:        []string{"event-consumers"},
			},
			"Status": {
				OperationID: "getKafkaOpenAPIStatus",
				Summary:     "Get status",
			},
		},
	}
}

func TestBuildOmitsKafkaConsumersFromMixedHTTPDocument(t *testing.T) {
	description, err := fb.DescribeReflectedAPI((*mixedKafkaOpenAPI)(nil))
	if err != nil {
		t.Fatalf("DescribeReflectedAPI() returned an error: %v", err)
	}
	document, err := Build(Config{Title: "Mixed API", Version: "1.0.0"}, []fb.APIDescription{description})
	if err != nil {
		t.Fatalf("Build() returned an error: %v", err)
	}
	if got, want := document.Paths.Len(), 1; got != want {
		t.Fatalf("OpenAPI path count = %d, want %d", got, want)
	}
	if document.Paths.Find("/status") == nil {
		t.Fatal("OpenAPI document does not contain HTTP /status path")
	}
	if document.Paths.Find("/Consume") != nil {
		t.Fatal("OpenAPI document exposes Kafka consumer as /Consume")
	}
	if _, exists := document.Components.Schemas["kafkaOpenAPIEvent"]; exists {
		t.Fatal("OpenAPI components contain the Kafka-only event schema")
	}
	for _, tag := range document.Tags {
		if tag.Name == "event-consumers" {
			t.Fatal("OpenAPI document contains a Kafka-only operation tag")
		}
	}
}
