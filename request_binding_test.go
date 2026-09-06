package koba

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestNormalizeRequestBindingsRejectsNullableWireParameters(t *testing.T) {
	type input struct {
		Value *string
	}
	type contract struct {
		Value Binding[*string]
	}

	_, err := normalizeRequestBindings(
		contract{Value: Query[*string]("value", Nullable())},
		reflect.TypeFor[input](),
	)
	if err == nil || !strings.Contains(err.Error(), "nullable is only supported for body and body-value bindings") {
		t.Fatalf("normalizeRequestBindings() error = %v, want unsupported nullable query error", err)
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

func TestNormalizeRequestBindingsAcceptsOneAuthenticatedSubjectOutsideBody(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	type input struct {
		Subject string
		Payload payload
	}
	type payloadContract struct {
		Name Binding[string]
	}
	type contract struct {
		Subject Binding[string]
		Payload BodyObjectBinding[payloadContract]
	}

	bindings, err := normalizeRequestBindings(contract{
		Subject: Subject(),
		Payload: BodyObject(payloadContract{Name: Value[string](Required())}, Required()),
	}, reflect.TypeFor[input]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if got, want := len(bindings), 3; got != want {
		t.Fatalf("binding count = %d, want %d", got, want)
	}
	subject := bindings[0]
	if subject.Source != BindingSourceSubject || subject.Name != "" || !reflect.DeepEqual(subject.FieldPath, []string{"Subject"}) || !subject.Input.Required || subject.Input.GoType != reflect.TypeFor[string]() {
		t.Fatalf("subject binding = %#v", subject)
	}
	if subject.Input.MinLength == nil || *subject.Input.MinLength != 1 {
		t.Fatalf("subject minimum length = %#v, want 1", subject.Input.MinLength)
	}
}

func TestNormalizeRequestBindingsRejectsDuplicateOrBodySubject(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		type input struct {
			First  string
			Second string
		}
		type contract struct {
			First  Binding[string]
			Second Binding[string]
		}
		_, err := normalizeRequestBindings(
			contract{First: Subject(), Second: Subject()},
			reflect.TypeFor[input](),
		)
		if err == nil || !strings.Contains(err.Error(), "both bind the authenticated subject") {
			t.Fatalf("normalizeRequestBindings() error = %v, want duplicate subject error", err)
		}
	})

	t.Run("inside body", func(t *testing.T) {
		type input struct {
			Subject string `json:"subject"`
		}
		type contract struct {
			Subject Binding[string]
		}
		_, err := normalizeRequestBindings(
			BodyObject(contract{Subject: Subject()}, Required()),
			reflect.TypeFor[input](),
		)
		if err == nil || !strings.Contains(err.Error(), "from inside a BodyObject") {
			t.Fatalf("normalizeRequestBindings() error = %v, want body subject error", err)
		}
	})
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

func TestBindingDocumentUsesValueSemanticsAndNormalizesMetadata(t *testing.T) {
	type input struct{ PageSize int }
	type contract struct{ PageSize Binding[int] }

	original := Query[int]("page_size", Min(1), Max(100))
	documented := original.Document(
		"  Maximum number of records returned.  ",
		Example(int8(20)),
		Deprecated(),
	)
	if original.requestBindingMetadata().documentation != nil {
		t.Fatal("Document mutated the original binding")
	}

	descriptor := contract{PageSize: documented}
	bindings, err := normalizeRequestBindings(descriptor, reflect.TypeFor[input]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	documentation := bindings[0].Documentation
	if documentation == nil {
		t.Fatal("normalized binding has no inline documentation")
	}
	if documentation.Description != "Maximum number of records returned." || !documentation.Deprecated {
		t.Fatalf("normalized documentation = %#v", documentation)
	}
	if documentation.Example != int(20) {
		t.Fatalf("normalized example = %#v, want int(20)", documentation.Example)
	}

	// A consumer may mutate a compiled plan without changing the authoring
	// descriptor used to compile another plan.
	documentation.Description = "changed"
	again, err := normalizeRequestBindings(descriptor, reflect.TypeFor[input]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() second call returned an error: %v", err)
	}
	if got := again[0].Documentation.Description; got != "Maximum number of records returned." {
		t.Fatalf("second normalized description = %q", got)
	}
}

func TestBindingDocumentAcceptsHTTPParameterSources(t *testing.T) {
	type input struct {
		Query  string
		Path   string
		Header string
		Cookie string
	}
	type contract struct {
		Query  Binding[string]
		Path   Binding[string]
		Header Binding[string]
		Cookie Binding[string]
	}

	bindings, err := normalizeRequestBindings(contract{
		Query:  Query[string]("query").Document("Query parameter."),
		Path:   Path[string]("path").Document("Path parameter."),
		Header: Header[string]("X-Header").Document("Header parameter."),
		Cookie: Cookie[string]("cookie").Document("Cookie parameter."),
	}, reflect.TypeFor[input]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if got, want := len(bindings), 4; got != want {
		t.Fatalf("binding count = %d, want %d", got, want)
	}
	for index, binding := range bindings {
		if binding.Documentation == nil || binding.Documentation.Description == "" {
			t.Errorf("binding %d documentation = %#v", index, binding.Documentation)
		}
	}
}

func TestBindingDocumentValidatesExamplesAgainstInputContract(t *testing.T) {
	type input struct{ PageSize int }
	type contract struct{ PageSize Binding[int] }

	for _, test := range []struct {
		name    string
		example any
		want    string
	}{
		{name: "incompatible type", example: "many", want: "cannot be represented by int"},
		{name: "violates minimum", example: 0, want: "greater than or equal to 1"},
		{name: "violates maximum", example: 101, want: "less than or equal to 100"},
		{name: "violates enum", example: 50, want: "must be one of"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeRequestBindings(
				contract{PageSize: Query[int]("page_size", Min(1), Max(100), OneOf(20, 100)).Document("Page size.", Example(test.example))},
				reflect.TypeFor[input](),
			)
			if err == nil || !strings.Contains(err.Error(), "binding documentation example") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeRequestBindings() error = %v, want inline example error containing %q", err, test.want)
			}
		})
	}
}

func TestBindingDocumentSupportsBodySourcesAndRejectsSubject(t *testing.T) {
	t.Run("subject", func(t *testing.T) {
		type input struct{ Subject string }
		type contract struct{ Subject Binding[string] }
		_, err := normalizeRequestBindings(
			contract{Subject: Subject().Document("Authenticated subject.")},
			reflect.TypeFor[input](),
		)
		if err == nil || !strings.Contains(err.Error(), `binding source "subject" does not support inline documentation`) {
			t.Fatalf("normalizeRequestBindings() error = %v, want unsupported subject documentation error", err)
		}
	})

	t.Run("body", func(t *testing.T) {
		bindings, err := normalizeRequestBindings(
			Body[string](Required()).Document("Request body."),
			reflect.TypeFor[string](),
		)
		if err != nil {
			t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
		}
		if got := bindings[0].Documentation; got == nil || got.Description != "Request body." {
			t.Fatalf("body documentation = %#v", got)
		}
	})

	t.Run("body value", func(t *testing.T) {
		type input struct{ Value string }
		type contract struct{ Value Binding[string] }
		bindings, err := normalizeRequestBindings(
			BodyObject(contract{Value: Value[string]().Document("Body value.")}),
			reflect.TypeFor[input](),
		)
		if err != nil {
			t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
		}
		if got := bindings[1].Documentation; got == nil || got.Description != "Body value." {
			t.Fatalf("body-value documentation = %#v", got)
		}
	})

	t.Run("blank description", func(t *testing.T) {
		type input struct{ Value string }
		type contract struct{ Value Binding[string] }
		_, err := normalizeRequestBindings(
			contract{Value: Query[string]("value").Document(" \t\n ")},
			reflect.TypeFor[input](),
		)
		if err == nil || !strings.Contains(err.Error(), "binding documentation description is required") {
			t.Fatalf("normalizeRequestBindings() error = %v, want blank description error", err)
		}
	})
}

func TestCompositeBindingDocumentationExampleIsOwned(t *testing.T) {
	example := map[string]any{"labels": []any{"original"}}
	bindings, err := normalizeRequestBindings(
		Body[map[string]any]().Document("Composite body.", Example(example)),
		reflect.TypeFor[map[string]any](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	example["labels"].([]any)[0] = "mutated"
	got := bindings[0].Documentation.Example.(map[string]any)["labels"].([]any)[0]
	if got != "original" {
		t.Fatalf("compiled example aliases author value: %v", got)
	}
}

func TestBindingDocumentationPreservesTimeExample(t *testing.T) {
	example := time.Date(2026, time.September, 6, 12, 30, 0, 123, time.UTC)
	bindings, err := normalizeRequestBindings(
		Body[time.Time]().Document("Timestamp.", Example(example)),
		reflect.TypeFor[time.Time](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if got, ok := bindings[0].Documentation.Example.(time.Time); !ok || !got.Equal(example) {
		t.Fatalf("compiled time example = %#v, want %v", bindings[0].Documentation.Example, example)
	}
}

type privateStateExample struct {
	Labels map[string]string `json:"labels"`
	cache  string
}

func TestBindingDocumentationPreservesPrivateStateAndOwnsPublicMutableData(t *testing.T) {
	example := privateStateExample{
		Labels: map[string]string{"mode": "original"},
		cache:  "opaque",
	}
	bindings, err := normalizeRequestBindings(
		Body[privateStateExample]().Document("Domain value.", Example(example)),
		reflect.TypeFor[privateStateExample](),
	)
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	example.Labels["mode"] = "mutated"
	got := bindings[0].Documentation.Example.(privateStateExample)
	if got.cache != "opaque" || got.Labels["mode"] != "original" {
		t.Fatalf("compiled private-state example = %#v", got)
	}
}

func TestNormalizeResolvedRequestBindingsNormalizesInlineDocumentation(t *testing.T) {
	type input struct{ PageSize int }

	original := &BindingDocumentation{
		Description: "  Maximum number of records returned.  ",
		Deprecated:  true,
		Example:     int8(20),
	}
	bindings, err := normalizeResolvedRequestBindings([]RequestBinding{{
		Source:        BindingSourceQuery,
		Name:          "page_size",
		FieldPath:     []string{"PageSize"},
		FieldIndex:    []int{0},
		Input:         InputOf[int](Min(1), Max(100)),
		Documentation: original,
	}}, reflect.TypeFor[input]())
	if err != nil {
		t.Fatalf("normalizeResolvedRequestBindings() returned an error: %v", err)
	}
	documentation := bindings[0].Documentation
	if documentation == original {
		t.Fatal("normalizeResolvedRequestBindings() retained the caller's documentation pointer")
	}
	if documentation.Description != "Maximum number of records returned." || !documentation.Deprecated || documentation.Example != int(20) {
		t.Fatalf("normalized documentation = %#v", documentation)
	}
	if original.Description != "  Maximum number of records returned.  " || original.Example != int8(20) {
		t.Fatalf("normalizeResolvedRequestBindings() mutated input documentation: %#v", original)
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

func TestNormalizeResolvedRequestBindingsRejectsInvalidSubjectBindings(t *testing.T) {
	type payload struct{ Subject string }
	type input struct {
		Subject string
		Other   string
		Payload payload
	}
	requiredSubject := func(path string, index []int) RequestBinding {
		return RequestBinding{
			Source:     BindingSourceSubject,
			FieldPath:  []string{path},
			FieldIndex: index,
			Input:      InputOf[string](Required(), MinLength(1)),
		}
	}

	for _, test := range []struct {
		name     string
		bindings []RequestBinding
		want     string
	}{
		{
			name: "duplicate",
			bindings: []RequestBinding{
				requiredSubject("Subject", []int{0}),
				requiredSubject("Other", []int{1}),
			},
			want: "both bind the authenticated subject",
		},
		{
			name: "named",
			bindings: []RequestBinding{{
				Source: BindingSourceSubject, Name: "sub", FieldPath: []string{"Subject"}, FieldIndex: []int{0}, Input: InputOf[string](Required()),
			}},
			want: "unexpected name",
		},
		{
			name: "optional",
			bindings: []RequestBinding{{
				Source: BindingSourceSubject, FieldPath: []string{"Subject"}, FieldIndex: []int{0}, Input: InputOf[string](),
			}},
			want: "must be required",
		},
		{
			name: "inside body",
			bindings: []RequestBinding{
				{Source: BindingSourceBody, FieldPath: []string{"Payload"}, FieldIndex: []int{2}, Input: InputOf[payload](Required())},
				{Source: BindingSourceSubject, FieldPath: []string{"Payload", "Subject"}, FieldIndex: []int{2, 0}, Input: InputOf[string](Required())},
			},
			want: "inside body object",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeResolvedRequestBindings(test.bindings, reflect.TypeFor[input]())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeResolvedRequestBindings() error = %v, want %q", err, test.want)
			}
		})
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

type objectRuleNestedInput struct {
	Note string `json:"note"`
}

type objectRuleInput struct {
	Name    string                `json:"name"`
	Details objectRuleNestedInput `json:"details"`
	Any     any                   `json:"any"`
	Hidden  string                `json:"-"`
}

type objectRuleDescriptor struct {
	Name Binding[string]
}

func TestNormalizeRequestBindingsNormalizesBodyObjectRules(t *testing.T) {
	descriptor := BodyObject(objectRuleDescriptor{
		Name: Value[string](),
	}).WithRules(
		AtLeastOne(" Name ", " Details.Note "),
		AllOrNone("Name", "Details.Note"),
		SameNullState("Name", "Details.Note"),
	)

	bindings, err := normalizeRequestBindings(descriptor, reflect.TypeFor[objectRuleInput]())
	if err != nil {
		t.Fatalf("normalizeRequestBindings() returned an error: %v", err)
	}
	if len(bindings) == 0 || bindings[0].Source != BindingSourceBody {
		t.Fatalf("bindings = %#v, want a body binding first", bindings)
	}
	rules := bindings[0].ObjectRules
	if len(rules) != 3 {
		t.Fatalf("object rules = %#v, want three rules", rules)
	}
	if rules[0].Kind() != ObjectRuleAtLeastOne || !reflect.DeepEqual(rules[0].Fields(), []string{"Name", "Details.Note"}) {
		t.Fatalf("first object rule = %#v/%v", rules[0], rules[0].Fields())
	}
	if rules[1].Kind() != ObjectRuleAllOrNone || !reflect.DeepEqual(rules[1].Fields(), []string{"Name", "Details.Note"}) {
		t.Fatalf("second object rule = %#v/%v", rules[1], rules[1].Fields())
	}
	if rules[2].Kind() != ObjectRuleSameNullState || !reflect.DeepEqual(rules[2].Fields(), []string{"Name", "Details.Note"}) {
		t.Fatalf("third object rule = %#v/%v", rules[2], rules[2].Fields())
	}

	fields := rules[0].Fields()
	fields[0] = "mutated"
	if rules[0].Fields()[0] != "Name" {
		t.Fatal("ObjectRule.Fields exposed mutable rule state")
	}
}

func TestNormalizeRequestBindingsRejectsInvalidBodyObjectRules(t *testing.T) {
	for _, test := range []struct {
		name string
		rule ObjectRule
		want string
	}{
		{name: "zero rule", rule: ObjectRule{}, want: "unsupported kind"},
		{name: "empty at least one", rule: AtLeastOne(), want: "requires at least 1 field"},
		{name: "short all or none", rule: AllOrNone("Name"), want: "requires at least 2 field"},
		{name: "short same null state", rule: SameNullState("Name"), want: "requires at least 2 field"},
		{name: "untyped same null state", rule: SameNullState("Name", "Any"), want: `field "Any" must have a concrete Go type`},
		{name: "duplicate", rule: AtLeastOne("Name", "Name"), want: `field "Name" is repeated`},
		{name: "missing", rule: AtLeastOne("Missing"), want: `field "Missing" does not exist`},
		{name: "excluded", rule: AtLeastOne("Hidden"), want: `field "Hidden" is excluded from JSON`},
		{name: "empty segment", rule: AtLeastOne("Details..Note"), want: "contains an empty segment"},
		{name: "non struct", rule: AtLeastOne("Name.Value"), want: "traverses non-struct type string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeRequestBindings(
				BodyObject(objectRuleDescriptor{Name: Value[string]()}).WithRules(test.rule),
				reflect.TypeFor[objectRuleInput](),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeRequestBindings() error = %v, want %q", err, test.want)
			}
		})
	}
}
