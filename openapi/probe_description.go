package openapi

import (
	"net/http"
	"reflect"
	"strings"

	fb "github.com/roxiegrillo/koba/v2"
	fbtypes "github.com/roxiegrillo/koba/v2/types"
)

const (
	defaultHealthPath    = "/healthz"
	defaultReadinessPath = "/readyz"
	healthOperationID    = "healthz_check"
	readinessOperationID = "readyz_check"
)

// withDefaultProbeDescriptions returns a copy of descriptions containing the
// framework-owned liveness and readiness operations. An explicitly described
// GET on either reserved path wins for that path, matching runtime precedence.
func withDefaultProbeDescriptions(descriptions []fb.APIDescription) []fb.APIDescription {
	result := append([]fb.APIDescription(nil), descriptions...)
	healthPresent := hasDescribedOperation(result, http.MethodGet, defaultHealthPath)
	readinessPresent := hasDescribedOperation(result, http.MethodGet, defaultReadinessPath)
	if healthPresent && readinessPresent {
		return result
	}

	operations := make([]fb.OperationDescription, 0, 2)
	if !healthPresent {
		operations = append(operations, defaultHealthDescription())
	}
	if !readinessPresent {
		operations = append(operations, defaultReadinessDescription())
	}
	return append(result, fb.APIDescription{
		Name:        "system",
		Description: "Framework-owned liveness and dependency readiness probes.",
		Operations:  operations,
	})
}

func hasDescribedOperation(descriptions []fb.APIDescription, method, route string) bool {
	for _, description := range descriptions {
		for _, operation := range description.Operations {
			if strings.EqualFold(strings.TrimSpace(operation.HTTPMethod), method) &&
				strings.TrimSpace(operation.Route) == route {
				return true
			}
		}
	}
	return false
}

func defaultHealthDescription() fb.OperationDescription {
	return fb.OperationDescription{
		MethodName:  "Health",
		OperationID: healthOperationID,
		Route:       defaultHealthPath,
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[fbtypes.ProbeStatus](),
		Documentation: fb.OperationDocumentation{
			OperationID: healthOperationID,
			Summary:     "Check liveness",
			Description: "Confirms that the process can serve HTTP requests.",
			Tags:        []string{"system"},
			Responses: map[int]fb.ResponseDocumentation{
				http.StatusOK: {Description: "Process is alive"},
			},
			Public: true,
		},
	}
}

func defaultReadinessDescription() fb.OperationDescription {
	return fb.OperationDescription{
		MethodName:  "Ready",
		OperationID: readinessOperationID,
		Route:       defaultReadinessPath,
		HTTPMethod:  http.MethodGet,
		OutputType:  reflect.TypeFor[fbtypes.ProbeStatus](),
		Documentation: fb.OperationDocumentation{
			OperationID: readinessOperationID,
			Summary:     "Check readiness",
			Description: "Confirms that every configured dependency check passes before the service receives traffic.",
			Tags:        []string{"system"},
			Responses: map[int]fb.ResponseDocumentation{
				http.StatusOK: {Description: "Service is ready for traffic"},
				http.StatusServiceUnavailable: {
					Description: "A required dependency is not ready",
					GoType:      reflect.TypeFor[fbtypes.ProbeStatus](),
				},
			},
			Public: true,
		},
	}
}
