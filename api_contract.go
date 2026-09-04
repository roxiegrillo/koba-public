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
	// Request maps transport values directly into the operation's request
	// argument. It is normally a companion struct whose exported fields mirror
	// the request type and whose leaves are Binding[T] values. Inputs remains
	// available for the legacy semantic-input/context binding model.
	Request       any
	Authorization AuthorizationContract
}

// AuthorizationContract describes the transport-neutral authorization rules
// which must be satisfied before an operation may execute. Authentication
// schemes and challenge details remain transport metadata.
type AuthorizationContract struct {
	// RequiredScopes uses AND semantics: an authenticated principal must hold
	// every listed scope. Scope matching is exact and case-sensitive.
	RequiredScopes []string
}

// InputContract describes one scalar operation input. Constraint values are
// normalized to GoType when the API is compiled.
type InputContract struct {
	GoType reflect.Type

	Required   bool
	Default    any
	HasDefault bool
	Minimum    any
	Maximum    any
	MinLength  *uint64
	MaxLength  *uint64
	Enum       []any
	Pattern    string
	Format     string
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
