package openapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

const schemaReferencePrefix = "#/components/schemas/"

// pruneUnreachableSchemas keeps components that are reachable from the public
// document and their transitive schema references. Contract projections are
// operation-local, so reflected source components may otherwise remain as
// incomplete, misleading duplicates in the generated document.
func pruneUnreachableSchemas(document *openapi3.T) error {
	if document == nil || document.Components == nil || len(document.Components.Schemas) == 0 {
		return nil
	}

	root := *document
	components := *document.Components
	components.Schemas = make(openapi3.Schemas)
	root.Components = &components
	references, err := schemaReferencesIn(&root)
	if err != nil {
		return fmt.Errorf("inspect document roots: %w", err)
	}

	reachable := make(map[string]struct{}, len(references))
	queue := append([]string(nil), references...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, visited := reachable[name]; visited {
			continue
		}
		schema := document.Components.Schemas[name]
		if schema == nil {
			// Leave dangling references for the existing resolver and validator to
			// report with their normal, more precise error messages.
			continue
		}
		reachable[name] = struct{}{}
		children, childErr := schemaReferencesIn(schema)
		if childErr != nil {
			return fmt.Errorf("inspect schema %q: %w", name, childErr)
		}
		queue = append(queue, children...)
	}

	for name := range document.Components.Schemas {
		if _, keep := reachable[name]; !keep {
			delete(document.Components.Schemas, name)
		}
	}
	return nil
}

func schemaReferencesIn(value any) ([]string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	collectSchemaReferences(document, seen)
	references := make([]string, 0, len(seen))
	for reference := range seen {
		references = append(references, reference)
	}
	return references, nil
}

func collectSchemaReferences(value any, references map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			collectSchemaReferences(child, references)
		}
	case []any:
		for _, child := range typed {
			collectSchemaReferences(child, references)
		}
	case string:
		if name, ok := schemaNameFromReference(typed); ok {
			references[name] = struct{}{}
		}
	}
}

func schemaNameFromReference(reference string) (string, bool) {
	if !strings.HasPrefix(reference, schemaReferencePrefix) {
		return "", false
	}
	name := strings.TrimPrefix(reference, schemaReferencePrefix)
	if separator := strings.IndexByte(name, '/'); separator >= 0 {
		name = name[:separator]
	}
	decoded, err := url.PathUnescape(name)
	if err != nil || decoded == "" {
		return "", false
	}
	decoded = strings.ReplaceAll(decoded, "~1", "/")
	decoded = strings.ReplaceAll(decoded, "~0", "~")
	return decoded, true
}
