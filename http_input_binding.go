package koba

import (
	"fmt"
	"net/http"
	"net/url"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"

	internalhttpscalar "github.com/roxiegrillo/koba/v2/internal/transport/httpscalar"
)

// bindHTTPInputs is the HTTP adapter half of the structural contract. It maps
// HTTP values to semantic identifiers and coerces them to their declared Go
// types. Defaults and constraints are then applied by the transport-neutral
// validator.
func bindHTTPInputs(operation OperationDescription, c *httprouter.Context) (OperationInputs, error) {
	if len(operation.Contract.Inputs) == 0 {
		return OperationInputs{}, nil
	}
	supplied := make(map[string]any, len(operation.Contract.Inputs))
	for _, parameter := range operation.Documentation.Parameters {
		if parameter.ContractInput == "" {
			continue
		}
		raw, present, err := httpParameterValue(c.Request, c.Params, parameter)
		if err != nil {
			return OperationInputs{}, err
		}
		if !present {
			continue
		}
		input := operation.Contract.Inputs[parameter.ContractInput]
		value, err := internalhttpscalar.Decode(raw, input.GoType)
		if err != nil {
			return OperationInputs{}, fmt.Errorf(
				"%s parameter %q: %w",
				parameter.In,
				parameter.Name,
				err,
			)
		}
		supplied[parameter.ContractInput] = value
	}
	return ValidateOperationInputs(operation.Contract, supplied)
}

func httpParameterValue(request *http.Request, path httprouter.Params, parameter ParameterDocumentation) (string, bool, error) {
	var values []string
	switch parameter.In {
	case ParameterInQuery:
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			return "", false, fmt.Errorf("query parameters are malformed: %w", err)
		}
		values = query[parameter.Name]
	case ParameterInHeader:
		values = request.Header.Values(parameter.Name)
	case ParameterInCookie:
		for _, cookie := range request.Cookies() {
			if cookie.Name == parameter.Name {
				values = append(values, cookie.Value)
			}
		}
	case ParameterInPath:
		value, exists := path.Get(parameter.Name)
		if exists {
			values = []string{value}
		}
	default:
		return "", false, fmt.Errorf("parameter %q has unsupported HTTP location %q", parameter.Name, parameter.In)
	}
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) > 1 {
		return "", false, fmt.Errorf("%s parameter %q must contain exactly one value", parameter.In, parameter.Name)
	}
	return values[0], true, nil
}
