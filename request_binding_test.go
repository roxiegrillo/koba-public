package koba

import (
	"reflect"
	"strings"
	"testing"
)

type requestBindingTestCoordinates struct {
	Latitude  float64
	Longitude float64
}

type requestBindingTestFilters struct {
	Status   string
	Location *requestBindingTestCoordinates
}

type requestBindingTestPayload struct {
	Title string
}

type requestBindingTestInput struct {
	ID      string
	Filters *requestBindingTestFilters
	TraceID string
	Session string
	Payload requestBindingTestPayload
	Ignored bool
}

type requestBindingTestCoordinateContract struct {
	Latitude  Binding[float64]
	Longitude Binding[float64]
}

type requestBindingTestFilterContract struct {
	Status   Binding[string]
	Location *requestBindingTestCoordinateContract
}

type requestBindingTestContract struct {
	ID      Binding[string]
	Filters requestBindingTestFilterContract
	TraceID Binding[string]
	Session Binding[string]
	Payload Binding[requestBindingTestPayload]
}

func TestNormalizeRequestBindingsWalksNestedStructsAndPointers(t *testing.T) {
	descriptor := requestBindingTestContract{
		ID: Path[string]("id", MinLength(1)),
		Filters: requestBindingTestFilterContract{
			Status: Query[string]("status", OneOf("active", "paused")),
			Location: &requestBindingTestCoordinateContract{
				Latitude:  Query[float64]("lat", Min(float64(-90)), Max(float64(90))),
				Longitude: Query[float64]("lon", Min(float64(-180)), Max(float64(180))),
			},
		},
		TraceID: Header[string]("X-Trace-ID", Required()),
		Session: Cookie[string]("session"),
		Payload: Body[requestBindingTestPayload](Required()),
	}

	bindings, err := normalizeRequestBindings(descriptor, reflect.TypeFor[*requestBindingTestInput]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if got, want := len(bindings), 7; got != want {
		t.Fatalf("binding count = %d, want %d", got, want)
	}

	want := []struct {
		path     string
		index    []int
		source   BindingSource
		name     string
		typeOf   reflect.Type
		required bool
	}{
		{path: "ID", index: []int{0}, source: BindingSourcePath, name: "id", typeOf: reflect.TypeFor[string](), required: true},
		{path: "Filters.Status", index: []int{1, 0}, source: BindingSourceQuery, name: "status", typeOf: reflect.TypeFor[string]()},
		{path: "Filters.Location.Latitude", index: []int{1, 1, 0}, source: BindingSourceQuery, name: "lat", typeOf: reflect.TypeFor[float64]()},
		{path: "Filters.Location.Longitude", index: []int{1, 1, 1}, source: BindingSourceQuery, name: "lon", typeOf: reflect.TypeFor[float64]()},
		{path: "TraceID", index: []int{2}, source: BindingSourceHeader, name: "X-Trace-ID", typeOf: reflect.TypeFor[string](), required: true},
		{path: "Session", index: []int{3}, source: BindingSourceCookie, name: "session", typeOf: reflect.TypeFor[string]()},
		{path: "Payload", index: []int{4}, source: BindingSourceBody, typeOf: reflect.TypeFor[requestBindingTestPayload](), required: true},
	}
	for index, expected := range want {
		binding := bindings[index]
		if got := strings.Join(binding.FieldPath, "."); got != expected.path {
			t.Errorf("binding %d path = %q, want %q", index, got, expected.path)
		}
		if !reflect.DeepEqual(binding.FieldIndex, expected.index) {
			t.Errorf("binding %s index = %v, want %v", expected.path, binding.FieldIndex, expected.index)
		}
		if binding.Source != expected.source || binding.Name != expected.name {
			t.Errorf("binding %s source/name = %q/%q, want %q/%q", expected.path, binding.Source, binding.Name, expected.source, expected.name)
		}
		if binding.Input.GoType != expected.typeOf || binding.Input.Required != expected.required {
			t.Errorf("binding %s input = type %v required %v, want type %v required %v", expected.path, binding.Input.GoType, binding.Input.Required, expected.typeOf, expected.required)
		}
	}
}

func TestNormalizeRequestBindingsAcceptsRootBody(t *testing.T) {
	bindings, err := normalizeRequestBindings(
		Body[requestBindingTestPayload](Required()),
		reflect.TypeFor[requestBindingTestPayload](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("binding count = %d, want 1", len(bindings))
	}
	if binding := bindings[0]; binding.Source != BindingSourceBody || len(binding.FieldPath) != 0 || len(binding.FieldIndex) != 0 || !binding.Input.Required {
		t.Fatalf("root binding = %+v", binding)
	}
}

type requestBindingTestBodyContract struct {
	Title Binding[string]
}

type requestBindingTestBodyOperationContract struct {
	ID      Binding[string]
	Payload BodyObjectBinding[requestBindingTestBodyContract]
}

func TestNormalizeRequestBindingsWalksBodyObjectValues(t *testing.T) {
	descriptor := requestBindingTestBodyOperationContract{
		ID: Path[string]("id"),
		Payload: BodyObject(requestBindingTestBodyContract{
			Title: Value[string](Required(), MinLength(1)),
		}),
	}

	bindings, err := normalizeRequestBindings(descriptor, reflect.TypeFor[requestBindingTestInput]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if got, want := len(bindings), 3; got != want {
		t.Fatalf("binding count = %d, want %d", got, want)
	}
	body := bindings[1]
	if body.Source != BindingSourceBody || strings.Join(body.FieldPath, ".") != "Payload" || !reflect.DeepEqual(body.FieldIndex, []int{4}) || body.Input.GoType != reflect.TypeFor[requestBindingTestPayload]() {
		t.Fatalf("body object binding = %+v", body)
	}
	title := bindings[2]
	if title.Source != BindingSourceValue || strings.Join(title.FieldPath, ".") != "Payload.Title" || !reflect.DeepEqual(title.FieldIndex, []int{4, 0}) || !title.Input.Required || title.Input.MinLength == nil || *title.Input.MinLength != 1 {
		t.Fatalf("body value binding = %+v", title)
	}
}

func TestNormalizeRequestBindingsNormalizesRules(t *testing.T) {
	type input struct{ PageSize int }
	type contract struct{ PageSize Binding[int] }

	bindings, err := normalizeRequestBindings(
		contract{PageSize: Query[int]("page_size", Default(int8(20)), Min(int8(1)), Max(int8(100)))},
		reflect.TypeFor[input](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	inputContract := bindings[0].Input
	if inputContract.Default != int(20) || inputContract.Minimum != int(1) || inputContract.Maximum != int(100) {
		t.Fatalf("normalized rules = default %#v min %#v max %#v", inputContract.Default, inputContract.Minimum, inputContract.Maximum)
	}
}

func TestNormalizeRequestBindingsNormalizesPointerScalarRules(t *testing.T) {
	type input struct{ PageSize *int }
	type contract struct{ PageSize Binding[*int] }

	bindings, err := normalizeRequestBindings(
		contract{PageSize: Query[*int]("page_size", Default(20), Min(1), Max(100), OneOf(20, 50))},
		reflect.TypeFor[input](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	inputContract := bindings[0].Input
	defaultValue, ok := inputContract.Default.(*int)
	if !ok || defaultValue == nil || *defaultValue != 20 {
		t.Fatalf("pointer default = %#v, want *int(20)", inputContract.Default)
	}
	if inputContract.Minimum != int(1) || inputContract.Maximum != int(100) || !reflect.DeepEqual(inputContract.Enum, []any{int(20), int(50)}) {
		t.Fatalf("pointer rules = min %#v max %#v enum %#v", inputContract.Minimum, inputContract.Maximum, inputContract.Enum)
	}
}

func TestNormalizeRequestBindingsRejectsShapeTypeAndRuleErrors(t *testing.T) {
	type nestedInput struct{ Value string }
	type input struct {
		Nested *nestedInput
		Count  int
	}

	tests := []struct {
		name       string
		descriptor any
		want       string
	}{
		{
			name: "missing field",
			descriptor: struct {
				Missing Binding[string]
			}{Missing: Query[string]("missing")},
			want: `field "Missing" does not exist`,
		},
		{
			name: "leaf type mismatch",
			descriptor: struct {
				Count Binding[int64]
			}{Count: Query[int64]("count")},
			want: `field "Count" binds int64, but request field has type int`,
		},
		{
			name: "nil nested descriptor",
			descriptor: struct {
				Nested *struct{ Value Binding[string] }
			}{},
			want: `field "Nested" is a nil pointer`,
		},
		{
			name: "invalid rule",
			descriptor: struct {
				Count Binding[int]
			}{Count: Query[int]("count", MinLength(1))},
			want: `field "Count": length and pattern constraints require a string`,
		},
		{
			name: "structured query",
			descriptor: struct {
				Nested Binding[*nestedInput]
			}{Nested: Query[*nestedInput]("nested")},
			want: `query bindings require a scalar`,
		},
		{
			name: "uninitialized leaf",
			descriptor: struct {
				Count Binding[int]
			}{},
			want: `field "Count" has an uninitialized Binding`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeRequestBindings(test.descriptor, reflect.TypeFor[input]())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeRequestBindings() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeRequestBindingsRejectsDuplicateWireAndBodyBindings(t *testing.T) {
	type input struct {
		First  string
		Second string
	}

	for _, test := range []struct {
		name       string
		descriptor any
		want       string
	}{
		{
			name: "header names are case insensitive",
			descriptor: struct {
				First  Binding[string]
				Second Binding[string]
			}{First: Header[string]("X-Trace-ID"), Second: Header[string]("x-trace-id")},
			want: `both bind header`,
		},
		{
			name: "one body",
			descriptor: struct {
				First  Binding[string]
				Second Binding[string]
			}{First: Body[string](), Second: Body[string]()},
			want: `both bind the request body`,
		},
		{
			name: "value requires body object",
			descriptor: struct {
				First Binding[string]
			}{First: Value[string]()},
			want: `uses Value outside a BodyObject`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeRequestBindings(test.descriptor, reflect.TypeFor[input]())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeRequestBindings() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeRequestBindingsRejectsEmptyDescriptor(t *testing.T) {
	type input struct{ Value string }
	_, err := normalizeRequestBindings(struct{}{}, reflect.TypeFor[input]())
	if err == nil || !strings.Contains(err.Error(), "contains no bindings") {
		t.Fatalf("normalizeRequestBindings() error = %v, want empty descriptor error", err)
	}
}

func TestNormalizeResolvedRequestBindingsRejectsFieldPathIndexDrift(t *testing.T) {
	type input struct {
		First  string
		Second string
	}
	_, err := normalizeResolvedRequestBindings([]RequestBinding{{
		Source:     BindingSourceQuery,
		Name:       "value",
		FieldPath:  []string{"First"},
		FieldIndex: []int{1},
		Input:      InputOf[string](),
	}}, reflect.TypeFor[input]())
	if err == nil || !strings.Contains(err.Error(), `field index [1] for "Second"`) {
		t.Fatalf("normalizeResolvedRequestBindings() error = %v, want path/index drift error", err)
	}
}

func TestValidateAPIDescriptionRejectsUnboundParameterInRequestMode(t *testing.T) {
	type input struct{ PageSize int }
	err := ValidateAPIDescription(APIDescription{Operations: []OperationDescription{{
		MethodName:  "List",
		OperationID: "list",
		Route:       "/items",
		HTTPMethod:  "GET",
		InputType:   reflect.TypeFor[input](),
		RequestBindings: []RequestBinding{{
			Source:     BindingSourceQuery,
			Name:       "page_size",
			FieldPath:  []string{"PageSize"},
			FieldIndex: []int{0},
			Input:      InputOf[int](),
		}},
		Documentation: OperationDocumentation{Parameters: []ParameterDocumentation{{
			Name:   "unbound",
			In:     ParameterInQuery,
			GoType: reflect.TypeFor[string](),
		}}},
	}}})
	if err == nil || !strings.Contains(err.Error(), "parameter unbound has no request binding") {
		t.Fatalf("ValidateAPIDescription() error = %v, want unbound parameter error", err)
	}
}

func TestNormalizeOperationContractPreservesRequestCompanion(t *testing.T) {
	type input struct{ Value string }
	type contract struct{ Value Binding[string] }
	descriptor := contract{Value: Query[string]("value")}

	normalized, err := normalizeOperationContract(OperationContract{Request: descriptor})
	if err != nil {
		t.Fatalf("normalizeOperationContract() returned an error: %v", err)
	}
	if !reflect.DeepEqual(normalized.Request, descriptor) {
		t.Fatalf("normalized request = %#v, want %#v", normalized.Request, descriptor)
	}
}
