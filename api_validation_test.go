package koba

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestDescribeAPIRejectsDuplicateAndInvalidMetadata(t *testing.T) {
	tests := []struct {
		name          string
		documentation APIDocumentation
		wantError     string
	}{
		{
			name: "duplicate operation IDs",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First":  {OperationID: "sameOperation"},
				"Second": {OperationID: "sameOperation"},
			}},
			wantError: "duplicate operation ID",
		},
		{
			name: "unknown documented method",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"Missing": {Summary: "Not an endpoint"},
			}},
			wantError: "unknown API method",
		},
		{
			name: "invalid parameter location",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{{
					Name:   "filter",
					In:     ParameterLocation("body"),
					GoType: reflect.TypeFor[string](),
				}}},
			}},
			wantError: "invalid location",
		},
		{
			name: "untyped parameter",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{{
					Name: "filter",
					In:   ParameterInQuery,
				}}},
			}},
			wantError: "has no Go type",
		},
		{
			name: "unsupported success status",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					201: {Description: "Created"},
				}},
			}},
			wantError: "resolved success status",
		},
		{
			name: "case-insensitive duplicate headers",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Parameters: []ParameterDocumentation{
					ParameterOf[string]("X-Request-ID", ParameterInHeader, "", false),
					ParameterOf[string]("x-request-id", ParameterInHeader, "", false),
				}},
			}},
			wantError: "duplicate parameter",
		},
		{
			name: "invalid response header name",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					http.StatusNoContent: {Headers: map[string]HeaderDocumentation{
						"X Invalid": {Description: "Invalid header"},
					}},
				}},
			}},
			wantError: "invalid header name",
		},
		{
			name: "case-insensitive duplicate response headers",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					http.StatusNoContent: {Headers: map[string]HeaderDocumentation{
						"X-Next-Page": {Description: "Next page"},
						"x-next-page": {Description: "Duplicate next page"},
					}},
				}},
			}},
			wantError: "duplicate header names",
		},
		{
			name: "invalid response header pattern",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					http.StatusNoContent: {Headers: map[string]HeaderDocumentation{
						"Location": {Pattern: "["},
					}},
				}},
			}},
			wantError: "invalid pattern",
		},
		{
			name: "duplicate response header enum",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					http.StatusNoContent: {Headers: map[string]HeaderDocumentation{
						"Cache-Control": {Enum: []string{"no-store", "no-store"}},
					}},
				}},
			}},
			wantError: "duplicate enum value",
		},
		{
			name: "response header example outside enum",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Responses: map[int]ResponseDocumentation{
					http.StatusNoContent: {Headers: map[string]HeaderDocumentation{
						"Cache-Control": {Enum: []string{"no-store"}, Example: "public"},
					}},
				}},
			}},
			wantError: "example is not in its enum",
		},
		{
			name: "public with security override",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {
					Public:   true,
					Security: []SecurityRequirement{{"token"}},
				},
			}},
			wantError: "both Public and Security",
		},
		{
			name: "public with required scopes",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {Public: true, RequiredScopes: []string{"read:items"}},
			}},
			wantError: "both Public and RequiredScopes",
		},
		{
			name: "empty required scope",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{""}},
			}},
			wantError: "empty required scope",
		},
		{
			name: "required scope with whitespace",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{"read:items create:items"}},
			}},
			wantError: "invalid required scope",
		},
		{
			name: "required scope with quote",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{`read:"items`}},
			}},
			wantError: "invalid required scope",
		},
		{
			name: "required scope with backslash",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{`read:\items`}},
			}},
			wantError: "invalid required scope",
		},
		{
			name: "non-ASCII required scope",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{"lectura:artículos"}},
			}},
			wantError: "invalid required scope",
		},
		{
			name: "duplicate required scope",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
				"First": {RequiredScopes: []string{"read:items", "read:items"}},
			}},
			wantError: "duplicate required scope",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeAPI(validationFixtureAPI{documentation: test.documentation})
			if err == nil {
				t.Fatalf("DescribeAPI() succeeded, want an error containing %q", test.wantError)
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("DescribeAPI() error = %q, want it to contain %q", err, test.wantError)
			}
		})
	}
}

func TestDescribeAPIAllowsOptionalAuthenticationAndExplicitEmptyOverride(t *testing.T) {
	for _, security := range [][]SecurityRequirement{
		{{"bearerAuth"}, {}},
		{},
	} {
		_, err := DescribeAPI(validationFixtureAPI{documentation: APIDocumentation{
			Operations: map[string]OperationDocumentation{
				"First": {Security: security},
			},
		}})
		if err != nil {
			t.Fatalf("DescribeAPI() rejected Security %#v: %v", security, err)
		}
	}
}

func TestDescribeAPIRejectsAnonymousSecurityAlternativeWithRequiredScopes(t *testing.T) {
	_, err := DescribeAPI(validationFixtureAPI{documentation: APIDocumentation{
		Operations: map[string]OperationDocumentation{
			"First": {
				RequiredScopes: []string{"read:items"},
				Security:       []SecurityRequirement{{"bearerAuth"}, {}},
			},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "allows anonymous access") {
		t.Fatalf("DescribeAPI() error = %v, want anonymous-security error", err)
	}
}

func TestDescribeAPIResolvesAuthorizationFromAPIContract(t *testing.T) {
	description, err := DescribeAPI(contractValidationFixtureAPI{
		contract: APIContract{Operations: map[string]OperationContract{
			"First": {
				Authorization: AuthorizationContract{
					RequiredScopes: []string{"read:items"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("DescribeAPI() returned an error: %v", err)
	}
	if got := description.Operations[0].ResolvedRequiredScopes(); len(got) != 1 || got[0] != "read:items" {
		t.Fatalf("resolved required scopes = %v, want [read:items]", got)
	}
}

func TestDescribeAPIRejectsScopesDeclaredInContractAndDocumentation(t *testing.T) {
	_, err := DescribeAPI(contractValidationFixtureAPI{
		documentation: APIDocumentation{Operations: map[string]OperationDocumentation{
			"First": {RequiredScopes: []string{"legacy:items"}},
		}},
		contract: APIContract{Operations: map[string]OperationContract{
			"First": {
				Authorization: AuthorizationContract{
					RequiredScopes: []string{"read:items"},
				},
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "both APIContract and APIDocumentation") {
		t.Fatalf("DescribeAPI() error = %v, want ambiguous authorization declaration", err)
	}
}

func TestDescribeAPIValidatesSuccessStatusSemantics(t *testing.T) {
	tests := []struct {
		name          string
		documentation OperationDocumentation
		want          string
	}{
		{name: "status below 200", documentation: OperationDocumentation{SuccessStatus: 199}, want: "between 200 and 299"},
		{name: "status above 299", documentation: OperationDocumentation{SuccessStatus: 300}, want: "between 200 and 299"},
		{name: "204 with response", documentation: OperationDocumentation{SuccessStatus: http.StatusNoContent}, want: "cannot be used by an operation with a response value"},
		{name: "205 with response", documentation: OperationDocumentation{SuccessStatus: http.StatusResetContent}, want: "cannot be used by an operation with a response value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeAPI(successStatusFixtureAPI{documentation: test.documentation})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DescribeAPI() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestDescribeAPIValidatesContractBindings(t *testing.T) {
	validInput := InputOf[string](Required())
	tests := []struct {
		name          string
		documentation APIDocumentation
		contract      APIContract
		want          string
	}{
		{
			name:     "unknown contracted method",
			contract: APIContract{Operations: map[string]OperationContract{"Missing": {}}},
			want:     "contract references unknown API method",
		},
		{
			name: "unknown contract input",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "value", In: ParameterInQuery, ContractInput: "missing",
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"value": validInput}}}},
			want:     "unknown contract input",
		},
		{
			name:     "unbound contract input",
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"value": validInput}}}},
			want:     "has no HTTP parameter binding",
		},
		{
			name: "mixed structural sources",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "value", In: ParameterInQuery, ContractInput: "value", GoType: reflect.TypeFor[string](),
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"value": validInput}}}},
			want:     "mixes ContractInput",
		},
		{
			name: "optional path input",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "value", In: ParameterInPath, ContractInput: "value",
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"value": InputOf[string]()}}}},
			want:     "must bind a required contract input",
		},
		{
			name: "HTTP binding cannot decode collection",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "values", In: ParameterInQuery, ContractInput: "values",
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"values": InputOf[[]string](Required())}}}},
			want:     "does not support HTTP scalar decoding",
		},
		{
			name: "HTTP binding rejects structured TextUnmarshaler",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "value", In: ParameterInQuery, ContractInput: "value",
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"value": InputOf[textArrayScalar]()}}}},
			want:     "does not support HTTP scalar decoding",
		},
		{
			name: "contract example has incompatible type",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "page_size", In: ParameterInQuery, ContractInput: "pageSize", Example: "many",
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"pageSize": InputOf[int](Min(1), Max(100))}}}},
			want:     "parameter page_size example",
		},
		{
			name: "contract example violates constraints",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "page_size", In: ParameterInQuery, ContractInput: "pageSize", Example: 101,
			}}}}},
			contract: APIContract{Operations: map[string]OperationContract{"First": {Inputs: map[string]InputContract{"pageSize": InputOf[int](Min(1), Max(100))}}}},
			want:     "less than or equal to 100",
		},
		{
			name: "legacy parameter example violates enum",
			documentation: APIDocumentation{Operations: map[string]OperationDocumentation{"First": {Parameters: []ParameterDocumentation{{
				Name: "mode", In: ParameterInQuery, GoType: reflect.TypeFor[string](), Enum: []any{"safe"}, Example: "fast",
			}}}}},
			want: "must be one of",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DescribeAPI(contractValidationFixtureAPI{documentation: test.documentation, contract: test.contract})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DescribeAPI() error = %v, want %q", err, test.want)
			}
		})
	}
}
