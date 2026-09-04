// Package staticgen contains the implementation behind the public staticgen
// facade.
package staticgen

import (
	"bytes"
	"fmt"
	"go/format"
	"reflect"
	"sort"
	"strings"

	fb "github.com/roxiegrillo/koba/v2"
)

const defaultBuildTag = "koba_static"

// Config carries the public facade's generation inputs into the internal
// implementation without coupling the public Config type to this package.
type Config struct {
	PackageName string
	PackagePath string
	BuildTag    string
	APIs        []any
}

// Generate returns a gofmt-formatted generated provider source file.
func Generate(config Config) ([]byte, error) {
	config.PackageName = strings.TrimSpace(config.PackageName)
	config.PackagePath = strings.TrimSpace(config.PackagePath)
	config.BuildTag = strings.TrimSpace(config.BuildTag)
	if config.PackageName == "" {
		return nil, fmt.Errorf("koba/staticgen: package name is required")
	}
	if config.PackagePath == "" {
		return nil, fmt.Errorf("koba/staticgen: package path is required")
	}
	if config.BuildTag == "" {
		config.BuildTag = defaultBuildTag
	}
	if len(config.APIs) == 0 {
		return nil, fmt.Errorf("koba/staticgen: at least one API is required")
	}

	imports := &importSet{current: config.PackagePath, paths: map[string]string{
		"github.com/roxiegrillo/koba/v2": "fb",
	}}
	type apiDefinition struct {
		typeOf      reflect.Type
		description fb.APIDescription
	}
	definitions := make([]apiDefinition, 0, len(config.APIs))
	seenTypes := make(map[string]struct{}, len(config.APIs))
	for index, api := range config.APIs {
		apiType := reflect.TypeOf(api)
		if apiType == nil {
			return nil, fmt.Errorf("koba/staticgen: API %d is nil; pass a value or typed nil pointer", index+1)
		}
		namedType := apiType
		for namedType.Kind() == reflect.Pointer {
			namedType = namedType.Elem()
		}
		if namedType.Name() == "" || namedType.PkgPath() != config.PackagePath {
			return nil, fmt.Errorf(
				"koba/staticgen: API %d type %s must be a named type from %s",
				index+1,
				apiType,
				config.PackagePath,
			)
		}
		identity := namedType.PkgPath() + "." + namedType.Name()
		if _, duplicate := seenTypes[identity]; duplicate {
			return nil, fmt.Errorf("koba/staticgen: duplicate API type %s", identity)
		}
		seenTypes[identity] = struct{}{}
		description, err := fb.DescribeReflectedAPI(api)
		if err != nil {
			return nil, fmt.Errorf("koba/staticgen: describe %s: %w", identity, err)
		}
		for _, operation := range description.Operations {
			if operation.InputType != nil || operation.OutputType != nil || len(operation.Documentation.Parameters) != 0 || len(operation.Contract.Inputs) != 0 || len(operation.RequestBindings) != 0 {
				imports.paths["reflect"] = "reflect"
			}
			method, ok := apiType.MethodByName(operation.MethodName)
			if !ok {
				return nil, fmt.Errorf("koba/staticgen: method %s.%s disappeared", identity, operation.MethodName)
			}
			inputs, _ := fb.GetInputOutputTypes(method)
			if body := bodyType(inputs); body != nil {
				if _, err := imports.typeExpression(body); err != nil {
					return nil, fmt.Errorf("koba/staticgen: %s.%s body: %w", identity, operation.MethodName, err)
				}
			}
			for _, contractType := range []reflect.Type{operation.InputType, operation.OutputType} {
				if contractType == nil {
					continue
				}
				if _, err := imports.typeExpression(contractType); err != nil {
					return nil, fmt.Errorf("koba/staticgen: %s.%s contract type: %w", identity, operation.MethodName, err)
				}
			}
			for _, parameter := range operation.Documentation.Parameters {
				if parameter.GoType != nil {
					if _, err := imports.typeExpression(parameter.GoType); err != nil {
						return nil, fmt.Errorf("koba/staticgen: %s.%s parameter %s: %w", identity, operation.MethodName, parameter.Name, err)
					}
				}
				if _, err := anyLiteral(parameter.Example); err != nil {
					return nil, fmt.Errorf("koba/staticgen: %s.%s parameter %s example: %w", identity, operation.MethodName, parameter.Name, err)
				}
				for enumIndex, value := range parameter.Enum {
					if _, err := anyLiteral(value); err != nil {
						return nil, fmt.Errorf("koba/staticgen: %s.%s parameter %s enum %d: %w", identity, operation.MethodName, parameter.Name, enumIndex+1, err)
					}
				}
			}
			contractInputs := make([]string, 0, len(operation.Contract.Inputs))
			for identifier := range operation.Contract.Inputs {
				contractInputs = append(contractInputs, identifier)
			}
			sort.Strings(contractInputs)
			for _, identifier := range contractInputs {
				input := operation.Contract.Inputs[identifier]
				if err := prepareInputContract(imports, input); err != nil {
					return nil, fmt.Errorf("koba/staticgen: %s.%s contract input %s: %w", identity, operation.MethodName, identifier, err)
				}
			}
			for bindingIndex, binding := range operation.RequestBindings {
				if err := prepareInputContract(imports, binding.Input); err != nil {
					return nil, fmt.Errorf(
						"koba/staticgen: %s.%s request binding %d: %w",
						identity,
						operation.MethodName,
						bindingIndex+1,
						err,
					)
				}
			}
		}
		definitions = append(definitions, apiDefinition{typeOf: apiType, description: description})
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].typeOf.String() < definitions[j].typeOf.String()
	})

	var source bytes.Buffer
	fmt.Fprintf(&source, "// Code generated by koba/staticgen. DO NOT EDIT.\n")
	fmt.Fprintf(&source, "//go:build %s\n\n", config.BuildTag)
	fmt.Fprintf(&source, "package %s\n\n", config.PackageName)
	writeImports(&source, imports.paths)
	for _, definition := range definitions {
		if err := writeAPI(&source, imports, definition.typeOf, definition.description); err != nil {
			return nil, err
		}
	}
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("koba/staticgen: format generated source: %w\n%s", err, source.String())
	}
	return formatted, nil
}

func prepareInputContract(imports *importSet, input fb.InputContract) error {
	if _, err := imports.typeExpression(input.GoType); err != nil {
		return err
	}
	for _, constraint := range []struct {
		label string
		value any
	}{{"default", input.Default}, {"minimum", input.Minimum}, {"maximum", input.Maximum}} {
		label, value := constraint.label, constraint.value
		if value == nil {
			continue
		}
		if _, err := anyLiteral(value); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	for enumIndex, value := range input.Enum {
		if _, err := anyLiteral(value); err != nil {
			return fmt.Errorf("enum %d: %w", enumIndex+1, err)
		}
	}
	return nil
}
