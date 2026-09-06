package kobaapp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	fb "github.com/roxiegrillo/koba/v2"
	"github.com/roxiegrillo/koba/v2/openapi"
)

type serviceValidationEvent struct {
	ID string `json:"id"`
}

type duplicateTopicFirstAPI struct{}

func (*duplicateTopicFirstAPI) ConsumeFirst(context.Context, serviceValidationEvent) error {
	return nil
}

func (*duplicateTopicFirstAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"ConsumeFirst": {Transport: fb.KafkaConsumer("items.changed")},
	}}
}

func (*duplicateTopicFirstAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"ConsumeFirst": {OperationID: "consumeFirstItemChange"},
	}}
}

type duplicateTopicSecondAPI struct{}

func (*duplicateTopicSecondAPI) ConsumeSecond(serviceValidationEvent) {}

func (*duplicateTopicSecondAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"ConsumeSecond": {Transport: fb.KafkaConsumer("items.changed")},
	}}
}

func (*duplicateTopicSecondAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"ConsumeSecond": {OperationID: "consumeSecondItemChange"},
	}}
}

func TestBuildArtifactsRejectsKafkaTopicConsumedByMultipleAPIs(t *testing.T) {
	apis := []any{(*duplicateTopicFirstAPI)(nil), (*duplicateTopicSecondAPI)(nil)}
	_, err := buildArtifacts(serviceValidationDefinition(t.TempDir(), apis))
	if err == nil || !strings.Contains(err.Error(), `Kafka topic "items.changed" is consumed by both`) {
		t.Fatalf("buildArtifacts() error = %v, want duplicate Kafka topic error", err)
	}
	if !strings.Contains(err.Error(), "duplicateTopicFirstAPI.ConsumeFirst") ||
		!strings.Contains(err.Error(), "duplicateTopicSecondAPI.ConsumeSecond") {
		t.Fatalf("buildArtifacts() error = %v, want both consumer locations", err)
	}
}

type duplicateOperationIDFirstAPI struct{}

func (*duplicateOperationIDFirstAPI) First() definitionFixtureResponse {
	return definitionFixtureResponse{}
}

func (*duplicateOperationIDFirstAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"First": {OperationID: "sharedOperation"},
	}}
}

type duplicateOperationIDSecondAPI struct{}

func (*duplicateOperationIDSecondAPI) Second() definitionFixtureResponse {
	return definitionFixtureResponse{}
}

func (*duplicateOperationIDSecondAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Second": {OperationID: "sharedOperation"},
	}}
}

func TestBuildArtifactsRejectsOperationIDSharedByMultipleAPIs(t *testing.T) {
	apis := []any{(*duplicateOperationIDFirstAPI)(nil), (*duplicateOperationIDSecondAPI)(nil)}
	_, err := buildArtifacts(serviceValidationDefinition(t.TempDir(), apis))
	if err == nil || !strings.Contains(err.Error(), `duplicate operation ID "sharedOperation"`) {
		t.Fatalf("buildArtifacts() error = %v, want duplicate operation ID error", err)
	}
	if !strings.Contains(err.Error(), "duplicateOperationIDFirstAPI.First") ||
		!strings.Contains(err.Error(), "duplicateOperationIDSecondAPI.Second") {
		t.Fatalf("buildArtifacts() error = %v, want both operation locations", err)
	}
}

type mixedServiceValidationAPI struct{}

func (*mixedServiceValidationAPI) Consume(context.Context, serviceValidationEvent) error {
	return nil
}

func (*mixedServiceValidationAPI) Status() definitionFixtureResponse {
	return definitionFixtureResponse{ID: "ready"}
}

func (*mixedServiceValidationAPI) Routes() map[string]string {
	return map[string]string{"Status": "/status"}
}

func (*mixedServiceValidationAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Consume": {Transport: fb.KafkaConsumer("mixed.events")},
	}}
}

func (*mixedServiceValidationAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Consume": {OperationID: "consumeMixedEvent"},
		"Status":  {OperationID: "getMixedStatus", Public: true},
	}}
}

type kafkaOnlyServiceValidationAPI struct{}

func (*kafkaOnlyServiceValidationAPI) Consume(serviceValidationEvent) {}

func (*kafkaOnlyServiceValidationAPI) APIContract() fb.APIContract {
	return fb.APIContract{Operations: map[string]fb.OperationContract{
		"Consume": {Transport: fb.KafkaConsumer("kafka-only.events")},
	}}
}

func (*kafkaOnlyServiceValidationAPI) APIDocumentation() fb.APIDocumentation {
	return fb.APIDocumentation{Operations: map[string]fb.OperationDocumentation{
		"Consume": {OperationID: "consumeKafkaOnlyEvent"},
	}}
}

func TestBuildArtifactsAcceptsMixedAndKafkaOnlyAPIs(t *testing.T) {
	tests := []struct {
		name string
		api  any
	}{
		{name: "mixed HTTP and Kafka", api: (*mixedServiceValidationAPI)(nil)},
		{name: "Kafka only", api: (*kafkaOnlyServiceValidationAPI)(nil)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifacts, err := buildArtifacts(serviceValidationDefinition(t.TempDir(), []any{test.api}))
			if err != nil {
				t.Fatalf("buildArtifacts() returned an error: %v", err)
			}
			if got, want := len(artifacts), 3; got != want {
				t.Fatalf("artifact count = %d, want %d", got, want)
			}
		})
	}
}

func serviceValidationDefinition(directory string, apis []any) Definition {
	return Definition{
		ID:             "service-validation-fixture",
		APIs:           apis,
		OpenAPI:        openapi.Config{Title: "Service validation fixture", Version: "1.0.0"},
		OpenAPIOutput:  filepath.Join(directory, "openapi.json"),
		ManifestOutput: filepath.Join(directory, "koba.lock.json"),
		StaticTargets: []StaticTarget{{
			PackageName: "kobaapp",
			PackagePath: "github.com/roxiegrillo/koba/v2/kobaapp",
			Output:      filepath.Join(directory, "zz_koba.gen.go"),
			APIs:        apis,
		}},
	}
}
