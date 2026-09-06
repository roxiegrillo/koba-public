package kobaapp

import (
	"fmt"
	"sort"

	fb "github.com/roxiegrillo/koba/v2"
)

func validateServiceAPIs(apis map[string]any) error {
	identities := make([]string, 0, len(apis))
	for identity := range apis {
		identities = append(identities, identity)
	}
	sort.Strings(identities)

	operationIDs := make(map[string]string)
	topics := make(map[string]string)
	for _, identity := range identities {
		// Definition is a build-time source contract. Always inspect the current
		// Go declarations instead of a possibly stale generated provider.
		description, err := fb.DescribeReflectedAPI(apis[identity])
		if err != nil {
			return fmt.Errorf("koba/kobaapp: describe API %s: %w", identity, err)
		}
		for _, operation := range description.Operations {
			location := identity + "." + operation.MethodName
			if previous, duplicate := operationIDs[operation.OperationID]; duplicate {
				return fmt.Errorf(
					"koba/kobaapp: duplicate operation ID %q on %s and %s",
					operation.OperationID,
					previous,
					location,
				)
			}
			operationIDs[operation.OperationID] = location

			if !operation.HasKafkaConsumerTransport() {
				continue
			}
			topic := operation.Contract.Transport.KafkaConsumer.Topic
			if previous, duplicate := topics[topic]; duplicate {
				return fmt.Errorf(
					"koba/kobaapp: Kafka topic %q is consumed by both %s and %s",
					topic,
					previous,
					location,
				)
			}
			topics[topic] = location
		}
	}
	return nil
}
