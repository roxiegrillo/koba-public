// Package staticgen generates reflection-free operation invokers for
// koba APIs. Reflection is used only while running the generator;
// the emitted provider calls API methods directly.
package staticgen

import (
	"bytes"
	"fmt"
	"go/format"
	"math"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"

	fb "github.com/roxiegrillo/koba/v2"
)

const DefaultBuildTag = "koba_static"

// Config selects the API values emitted into one generated Go package. All API
// types must be named types from PackagePath. Typed nil pointers are supported.
type Config struct {
	PackageName string
	PackagePath string
	BuildTag    string
	APIs        []any
}

type importSet struct {
	current string
	paths   map[string]string
}

var reservedImportAliases = map[string]struct{}{
	"api": {}, "invocation": {}, "request": {}, "value": {}, "err": {},
	"break": {}, "default": {}, "func": {}, "interface": {}, "select": {},
	"case": {}, "defer": {}, "go": {}, "map": {}, "struct": {}, "chan": {},
	"else": {}, "goto": {}, "package": {}, "switch": {}, "const": {},
	"fallthrough": {}, "if": {}, "range": {}, "type": {}, "continue": {},
	"for": {}, "import": {}, "return": {}, "var": {},
	"any": {}, "bool": {}, "byte": {}, "comparable": {}, "complex64": {},
	"complex128": {}, "error": {}, "float32": {}, "float64": {}, "int": {},
	"int8": {}, "int16": {}, "int32": {}, "int64": {}, "rune": {},
	"string": {}, "uint": {}, "uint8": {}, "uint16": {}, "uint32": {},
	"uint64": {}, "uintptr": {}, "true": {}, "false": {}, "iota": {}, "nil": {},
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
		config.BuildTag = DefaultBuildTag
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
			if operation.InputType != nil || operation.OutputType != nil || len(operation.Documentation.Parameters) != 0 {
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
				if _, err := imports.typeExpression(parameter.GoType); err != nil {
					return nil, fmt.Errorf("koba/staticgen: %s.%s parameter %s: %w", identity, operation.MethodName, parameter.Name, err)
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

func writeImports(source *bytes.Buffer, imports map[string]string) {
	standard := make([]string, 0, len(imports))
	external := make([]string, 0, len(imports))
	for importPath := range imports {
		if isStandardImportPath(importPath) {
			standard = append(standard, importPath)
		} else {
			external = append(external, importPath)
		}
	}
	sort.Strings(standard)
	sort.Strings(external)
	source.WriteString("import (\n")
	for groupIndex, group := range [][]string{standard, external} {
		if len(group) == 0 {
			continue
		}
		if groupIndex > 0 && len(standard) > 0 {
			source.WriteByte('\n')
		}
		for _, importPath := range group {
			fmt.Fprintf(source, "\t%s %s\n", imports[importPath], strconv.Quote(importPath))
		}
	}
	source.WriteString(")\n\n")
}

func isStandardImportPath(importPath string) bool {
	root, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(root, ".")
}

func writeAPI(source *bytes.Buffer, imports *importSet, apiType reflect.Type, description fb.APIDescription) error {
	receiverName := apiType.Name()
	receiverExpression := receiverName
	if apiType.Kind() == reflect.Pointer {
		receiverName = apiType.Elem().Name()
		receiverExpression = "*" + receiverName
	}
	fmt.Fprintf(source, "func (api %s) KobaCompiledAPI() fb.CompiledAPI {\n", receiverExpression)
	fmt.Fprintf(source, "\treturn fb.CompiledAPI{\n")
	fmt.Fprintf(source, "\t\tDescription: ")
	if err := writeDescription(source, imports, description); err != nil {
		return err
	}
	fmt.Fprintf(source, ",\n")
	fmt.Fprintf(source, "\t\tInvokers: map[string]fb.OperationInvoker{\n")
	for _, operation := range description.Operations {
		method, _ := apiType.MethodByName(operation.MethodName)
		inputs, outputs := fb.GetInputOutputTypes(method)
		fmt.Fprintf(source, "\t\t\t%q: func(invocation fb.Invocation) (fb.InvocationResult, error) {\n", operation.MethodName)
		body := bodyType(inputs)
		argument := ""
		if body != nil {
			expression, err := imports.typeExpression(body)
			if err != nil {
				return err
			}
			if body.Kind() == reflect.Pointer {
				expression, err = imports.typeExpression(body.Elem())
				if err != nil {
					return err
				}
				argument = "&request"
			} else {
				argument = "request"
			}
			fmt.Fprintf(source, "\t\t\t\tvar request %s\n", expression)
			fmt.Fprintf(source, "\t\t\t\tif err := invocation.Decode(&request); err != nil { return fb.InvocationResult{}, err }\n")
		}
		arguments := make([]string, 0, 2)
		if fb.HasContext(inputs) {
			arguments = append(arguments, "invocation.Context")
		}
		if argument != "" {
			arguments = append(arguments, argument)
		}
		call := fmt.Sprintf("api.%s(%s)", operation.MethodName, strings.Join(arguments, ", "))
		if len(outputs) == 0 {
			fmt.Fprintf(source, "\t\t\t\t%s\n", call)
			fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{}, nil\n")
		} else {
			fmt.Fprintf(source, "\t\t\t\tvalue := %s\n", call)
			if outputs[0].Kind() == reflect.Pointer {
				fmt.Fprintf(source, "\t\t\t\tif value == nil { return fb.InvocationResult{Value: nil, HasValue: true}, nil }\n")
				fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{Value: *value, HasValue: true}, nil\n")
			} else {
				fmt.Fprintf(source, "\t\t\t\treturn fb.InvocationResult{Value: value, HasValue: true}, nil\n")
			}
		}
		fmt.Fprintf(source, "\t\t\t},\n")
	}
	fmt.Fprintf(source, "\t\t},\n")
	fmt.Fprintf(source, "\t}\n")
	fmt.Fprintf(source, "}\n\n")
	return nil
}

func writeDescription(source *bytes.Buffer, imports *importSet, description fb.APIDescription) error {
	fmt.Fprintf(source, "fb.APIDescription{Name: %q, Description: %q, Operations: []fb.OperationDescription{", description.Name, description.Description)
	for _, operation := range description.Operations {
		fmt.Fprintf(source, "{MethodName: %q, OperationID: %q, Route: %q, HTTPMethod: %q", operation.MethodName, operation.OperationID, operation.Route, operation.HTTPMethod)
		if operation.InputType != nil {
			expression, err := imports.typeExpression(operation.InputType)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, ", InputType: reflect.TypeFor[%s]()", expression)
		}
		if operation.OutputType != nil {
			expression, err := imports.typeExpression(operation.OutputType)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, ", OutputType: reflect.TypeFor[%s]()", expression)
		}
		fmt.Fprintf(source, ", Documentation: ")
		if err := writeOperationDocumentation(source, imports, operation.Documentation); err != nil {
			return err
		}
		fmt.Fprintf(source, "},")
	}
	fmt.Fprintf(source, "}}")
	return nil
}

func writeOperationDocumentation(source *bytes.Buffer, imports *importSet, documentation fb.OperationDocumentation) error {
	fmt.Fprintf(source, "fb.OperationDocumentation{Method: %q, SuccessStatus: %d, OperationID: %q, Summary: %q, Description: %q", documentation.Method, documentation.SuccessStatus, documentation.OperationID, documentation.Summary, documentation.Description)
	if documentation.Tags != nil {
		fmt.Fprintf(source, ", Tags: %#v", documentation.Tags)
	}
	if documentation.Parameters != nil {
		fmt.Fprintf(source, ", Parameters: []fb.ParameterDocumentation{")
		for _, parameter := range documentation.Parameters {
			expression, err := imports.typeExpression(parameter.GoType)
			if err != nil {
				return err
			}
			fmt.Fprintf(source, "{Name: %q, In: fb.ParameterLocation(%q), Description: %q, Required: %t, Deprecated: %t, GoType: reflect.TypeFor[%s](), Format: %q, Pattern: %q", parameter.Name, parameter.In, parameter.Description, parameter.Required, parameter.Deprecated, expression, parameter.Format, parameter.Pattern)
			if parameter.Enum != nil {
				fmt.Fprintf(source, ", Enum: []any{")
				for _, value := range parameter.Enum {
					literal, err := anyLiteral(value)
					if err != nil {
						return err
					}
					fmt.Fprintf(source, "%s,", literal)
				}
				fmt.Fprintf(source, "}")
			}
			if parameter.Example != nil {
				literal, err := anyLiteral(parameter.Example)
				if err != nil {
					return err
				}
				fmt.Fprintf(source, ", Example: %s", literal)
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.RequestBodyDescription != "" {
		fmt.Fprintf(source, ", RequestBodyDescription: %q", documentation.RequestBodyDescription)
	}
	if documentation.Responses != nil {
		statuses := make([]int, 0, len(documentation.Responses))
		for status := range documentation.Responses {
			statuses = append(statuses, status)
		}
		sort.Ints(statuses)
		fmt.Fprintf(source, ", Responses: map[int]fb.ResponseDocumentation{")
		for _, status := range statuses {
			fmt.Fprintf(source, "%d: {Description: %q},", status, documentation.Responses[status].Description)
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.Deprecated {
		fmt.Fprintf(source, ", Deprecated: true")
	}
	if documentation.Security != nil {
		fmt.Fprintf(source, ", Security: []fb.SecurityRequirement{")
		for _, requirement := range documentation.Security {
			fmt.Fprintf(source, "{")
			for _, scheme := range requirement {
				fmt.Fprintf(source, "%q,", scheme)
			}
			fmt.Fprintf(source, "},")
		}
		fmt.Fprintf(source, "}")
	}
	if documentation.Public {
		fmt.Fprintf(source, ", Public: true")
	}
	fmt.Fprintf(source, "}")
	return nil
}

func anyLiteral(value any) (string, error) {
	if value == nil {
		return "nil", nil
	}
	return reflectedLiteral(reflect.ValueOf(value))
}

func reflectedLiteral(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "nil", nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "nil", nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return strconv.Quote(value.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Kind().String() + "(" + strconv.FormatInt(value.Int(), 10) + ")", nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Kind().String() + "(" + strconv.FormatUint(value.Uint(), 10) + ")", nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return "", fmt.Errorf("non-finite floating-point values are not supported")
		}
		return value.Kind().String() + "(" + strconv.FormatFloat(number, 'g', -1, value.Type().Bits()) + ")", nil
	case reflect.Slice, reflect.Array:
		var result strings.Builder
		result.WriteString("[]any{")
		for index := 0; index < value.Len(); index++ {
			literal, err := reflectedLiteral(value.Index(index))
			if err != nil {
				return "", err
			}
			result.WriteString(literal)
			result.WriteByte(',')
		}
		result.WriteByte('}')
		return result.String(), nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("example maps must have string keys, got %s", value.Type())
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		var result strings.Builder
		result.WriteString("map[string]any{")
		for _, key := range keys {
			literal, err := reflectedLiteral(value.MapIndex(key))
			if err != nil {
				return "", err
			}
			result.WriteString(strconv.Quote(key.String()))
			result.WriteByte(':')
			result.WriteString(literal)
			result.WriteByte(',')
		}
		result.WriteByte('}')
		return result.String(), nil
	default:
		return "", fmt.Errorf("example value type %s is not supported; use JSON scalar, list, or string-keyed map values", value.Type())
	}
}

func bodyType(inputs []reflect.Type) reflect.Type {
	if len(inputs) == 0 {
		return nil
	}
	start := 0
	if fb.HasContext(inputs) {
		start = 1
	}
	if len(inputs) == start {
		return nil
	}
	return inputs[start]
}

func (imports *importSet) typeExpression(value reflect.Type) (string, error) {
	if value.Name() != "" {
		if value.PkgPath() == "" {
			return value.Name(), nil
		}
		if value.PkgPath() == imports.current {
			return value.Name(), nil
		}
		alias := imports.alias(value.PkgPath())
		return alias + "." + value.Name(), nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		element, err := imports.typeExpression(value.Elem())
		return "*" + element, err
	case reflect.Slice:
		element, err := imports.typeExpression(value.Elem())
		return "[]" + element, err
	case reflect.Array:
		element, err := imports.typeExpression(value.Elem())
		return fmt.Sprintf("[%d]%s", value.Len(), element), err
	case reflect.Map:
		key, err := imports.typeExpression(value.Key())
		if err != nil {
			return "", err
		}
		element, err := imports.typeExpression(value.Elem())
		return "map[" + key + "]" + element, err
	}
	if value.Kind() == reflect.Interface && value.NumMethod() == 0 {
		return "any", nil
	}
	return "", fmt.Errorf("unnamed type %s is not supported", value)
}

func (imports *importSet) alias(importPath string) string {
	if alias, exists := imports.paths[importPath]; exists {
		return alias
	}
	base := sanitizeIdentifier(path.Base(importPath))
	alias := base
	used := make(map[string]struct{}, len(imports.paths))
	for _, existing := range imports.paths {
		used[existing] = struct{}{}
	}
	for suffix := 2; ; suffix++ {
		_, usedAlias := used[alias]
		_, reservedAlias := reservedImportAliases[alias]
		if !usedAlias && !reservedAlias {
			break
		}
		alias = base + strconv.Itoa(suffix)
	}
	imports.paths[importPath] = alias
	return alias
}

func sanitizeIdentifier(value string) string {
	var result strings.Builder
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			result.WriteRune(character)
		}
	}
	if result.Len() == 0 {
		return "pkg"
	}
	return result.String()
}
