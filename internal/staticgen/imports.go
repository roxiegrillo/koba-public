package staticgen

import (
	"bytes"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

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
