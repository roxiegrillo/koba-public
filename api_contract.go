package koba

import "reflect"

// APIContract describes the executable boundary between transports and API
// functions. Its operations are keyed by exported Go method name, just like
// APIDocumentation. Request descriptors bind transport values into ordinary
// typed function inputs; structural rules are shared by runtime validation and
// contract renderers.
type APIContract struct {
	Operations map[string]OperationContract
}

// OperationContract contains the request construction and authorization rules
// accepted by one operation.
type OperationContract struct {
	Inputs map[string]InputContract
	// Request maps transport and authenticated-context values directly into the
	// operation's request argument. It is normally a companion struct whose
	// exported fields mirror the request type and whose leaves are Binding[T]
	// values. Inputs remains available for the legacy semantic-input/context
	// binding model.
	Request any
	// Response optionally describes structural guarantees and human-facing
	// field metadata for the operation's successful response. It is a partial
	// companion value whose exported fields mirror the function return type and
	// whose leaves are ResponseValue[T] values. ResponseObject may be used when
	// an object field itself also needs metadata.
	Response      any
	Authorization AuthorizationContract
	// Transport selects how the operation is invoked. The zero value is an
	// ordinary HTTP request/response operation; explicit contracts describe
	// WebSocket upgrades and message-driven transports such as Kafka.
	Transport TransportContract
}

// AuthorizationContract describes the transport-neutral authorization rules
// which must be satisfied before an operation may execute. Authentication
// schemes and challenge details remain transport metadata.
type AuthorizationContract struct {
	// RequiredScopes uses AND semantics: an authenticated principal must hold
	// every listed scope. Scope matching is exact and case-sensitive.
	RequiredScopes []string
}

// InputContract describes one operation input. Constraint values are
// normalized to GoType when the API is compiled.
type InputContract struct {
	GoType reflect.Type

	Required   bool
	Nullable   bool
	Default    any
	HasDefault bool
	Minimum    any
	Maximum    any
	MinLength  *uint64
	MaxLength  *uint64
	MinItems   *uint64
	MaxItems   *uint64
	// UniqueItems requires every element of a JSON-array input to be distinct.
	// It is independent from collection cardinality.
	UniqueItems bool
	// Items describes each element of a JSON-array input. Collection
	// cardinality remains on this contract; element constraints and prose live
	// on the nested contract so transports can project them at the correct
	// schema level.
	Items   *CollectionItemContract
	Enum    []any
	Pattern string
	Format  string
}

// CollectionItemContract contains the executable structural rules and
// human-facing metadata shared by every element of a collection. Authors use
// Element for scalar elements and ElementObject for recursive object elements.
type CollectionItemContract struct {
	Input         InputContract
	Documentation *BindingDocumentation
	// Descriptor is the authoring-time companion for an object element. Its
	// leaves are Value bindings. Contract normalization compiles it into
	// Bindings and removes it so generated plans never retain application
	// companion values.
	Descriptor any
	// Bindings is the transport-neutral compiled field metadata for an object
	// element. Authors should leave it empty and use ElementObject.
	Bindings []ResponseBinding
}

// CollectionElement is a typed element descriptor consumed by Elements. It
// can describe either a scalar element (Element) or a recursively structured
// element (ElementObject).
type CollectionElement[T any] struct {
	contract      InputContract
	descriptor    any
	documentation *BindingDocumentation
}

// WithAPIContract is implemented by API values which declare executable
// boundary rules. Implementations should be metadata-only so they can safely be
// called on a typed nil receiver by build-time generators.
type WithAPIContract interface {
	APIContract() APIContract
}

// InputOption configures a typed InputContract created with InputOf.
type InputOption interface {
	applyInput(*InputContract)
}

type inputOptionFunc func(*InputContract)

func (option inputOptionFunc) applyInput(input *InputContract) {
	option(input)
}

// InputOf creates a structural input contract for T.
func InputOf[T any](options ...InputOption) InputContract {
	input := InputContract{GoType: reflect.TypeFor[T]()}
	for _, option := range options {
		if option != nil {
			option.applyInput(&input)
		}
	}
	return input
}

// Required marks an operation input as required.
func Required() InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Required = true })
}

// Nullable allows an explicitly supplied null value for a nil-capable input.
// Required and Nullable are independent: together they require the input to
// be present while permitting its value to be null.
func Nullable() InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Nullable = true })
}

// Default supplies the semantic value used when an input is absent.
func Default(value any) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		input.Default = value
		input.HasDefault = true
	})
}

// Min sets an inclusive numeric minimum.
func Min(value any) InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Minimum = value })
}

// Max sets an inclusive numeric maximum.
func Max(value any) InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Maximum = value })
}

// MinLength sets the minimum number of Unicode code points in a string.
func MinLength(value uint64) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		length := value
		input.MinLength = &length
	})
}

// MaxLength sets the maximum number of Unicode code points in a string.
func MaxLength(value uint64) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		length := value
		input.MaxLength = &length
	})
}

// MinItems sets the minimum number of elements in a slice input. Nullability
// remains independent from cardinality: Nullable permits null, while an empty
// non-null slice still has zero items.
func MinItems(value uint64) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		items := value
		input.MinItems = &items
	})
}

// MaxItems sets the maximum number of elements in a slice input.
func MaxItems(value uint64) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		items := value
		input.MaxItems = &items
	})
}

// UniqueItems requires a slice to contain no duplicate values. Equality uses
// the complete decoded Go value, including recursively structured elements.
func UniqueItems() InputOption {
	return inputOptionFunc(func(input *InputContract) { input.UniqueItems = true })
}

// Element creates the typed structural contract for each scalar value in a
// collection. Use ElementObject when T is a struct with field-level metadata.
func Element[T any](options ...InputOption) CollectionElement[T] {
	return CollectionElement[T]{contract: InputOf[T](options...)}
}

// ElementObject creates a typed structural contract for every object in a
// collection. descriptor is a recursive companion whose exported fields
// mirror T and whose leaves are Value bindings.
func ElementObject[T any](descriptor any, options ...InputOption) CollectionElement[T] {
	return CollectionElement[T]{
		contract:   InputOf[T](options...),
		descriptor: descriptor,
	}
}

// Document attaches human-facing metadata to a collection element schema.
func (element CollectionElement[T]) Document(
	description string,
	options ...BindingDocumentationOption,
) CollectionElement[T] {
	documentation := BindingDocumentation{Description: description}
	for _, option := range options {
		if option != nil {
			option.applyBindingDocumentation(&documentation)
		}
	}
	element.documentation = &documentation
	return element
}

// Elements attaches a typed element contract to a collection. The element's
// Go type must exactly match the collection element type.
func Elements[T any](element CollectionElement[T]) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		input.Items = &CollectionItemContract{
			Input:         element.contract,
			Documentation: cloneBindingDocumentation(element.documentation),
			Descriptor:    element.descriptor,
		}
	})
}

// OneOf restricts an input to the supplied scalar values.
func OneOf(values ...any) InputOption {
	return inputOptionFunc(func(input *InputContract) {
		// Preserve an explicitly empty set so contract validation can distinguish
		// OneOf() from an input without an enum constraint.
		input.Enum = append([]any{}, values...)
	})
}

// Matches restricts a string input using a regular expression.
func Matches(pattern string) InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Pattern = pattern })
}

// WithFormat annotates an input with a portable semantic format such as uuid.
// Format validation is transport or registry specific; the built-in structural
// validator does not infer validation behavior from this annotation.
func WithFormat(format string) InputOption {
	return inputOptionFunc(func(input *InputContract) { input.Format = format })
}
