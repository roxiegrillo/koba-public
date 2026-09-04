package openapi

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

var ginParameter = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_]*)`)

func normalizePath(route string) string {
	route = strings.TrimSpace(route)
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	return ginParameter.ReplaceAllString(route, `${1}{${2}}`)
}

var pathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

func validatePathParameters(methodName, route string, parameters openapi3.Parameters) error {
	declared := make(map[string]struct{})
	for _, parameter := range parameters {
		if parameter.Value != nil && parameter.Value.In == openapi3.ParameterInPath {
			declared[parameter.Value.Name] = struct{}{}
		}
	}
	for _, match := range pathParameter.FindAllStringSubmatch(route, -1) {
		if _, exists := declared[match[1]]; !exists {
			return fmt.Errorf("koba/openapi: %s route %s is missing path parameter metadata for %q", methodName, route, match[1])
		}
		delete(declared, match[1])
	}
	for name := range declared {
		return fmt.Errorf("koba/openapi: %s documents path parameter %q which is absent from %s", methodName, name, route)
	}
	return nil
}

func defaultTag(route string) string {
	segments := strings.Split(strings.Trim(route, "/"), "/")
	for _, segment := range segments {
		if segment == "" || strings.HasPrefix(segment, "{") || regexp.MustCompile(`^v[0-9]+$`).MatchString(segment) {
			continue
		}
		return segment
	}
	return "api"
}

var identifierBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func splitIdentifier(value string) string {
	value = identifierBoundary.ReplaceAllString(value, `$1 $2`)
	value = strings.ReplaceAll(value, "_", " ")
	return strings.TrimSpace(value)
}
