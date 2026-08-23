package plugin

import (
	"context"
	"errors"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/resource"
)

type CompileContext struct {
	context context.Context
	traits  traitStore
}
type SuggestContext struct{}

func (c CompileContext) traitSlots() traitStore { return c.traits }

// Context returns planning cancellation and deadline without context values.
// Compile remains a pure function of its explicit inputs and prepared traits.
func (c CompileContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

// CompileContextWithContext returns an immutable CompileContext carrying only
// Done, Err, and Deadline from parent. Value always returns nil.
func CompileContextWithContext(value CompileContext, parent context.Context) CompileContext {
	if parent == nil {
		parent = context.Background()
	}
	value.context = compileCancellationContext{Context: parent}
	return value
}

type compileCancellationContext struct{ context.Context }

func (compileCancellationContext) Value(any) any { return nil }

// ErrWorkerLimit is returned by TaskStarter.Start when a component has no
// unused worker capacity in the resource.Request declared by Compile.
var ErrWorkerLimit = errors.New("component worker grant is exhausted")

// TaskStarter starts component-owned background work under Host cancellation
// and join ownership. Compile must request at least the maximum number of
// concurrent tasks through resource.Request.Workers. Start returns
// ErrWorkerLimit when that grant is exhausted, including when no workers were
// requested.
type TaskStarter interface {
	Start(string, func(context.Context) error) error
}

type OpenServices struct {
	Buffers *buffer.Allocator
	// Tasks enforces the Workers grant returned by this component's Compile.
	Tasks       TaskStarter
	Diagnostics diagnostic.Sink
	// Owner is this component's own failure domain, and it lives as long as
	// the component does.
	Owner flow.Owner
	// Boundary is the one node-local Access/Endpoint binding selected by the
	// planner. It is not a general service bag.
	Boundary any
	// Source is the optional read-only source view associated with prepared
	// Format state. It is borrowed for this component's Open-to-Close lifetime.
	Source any
	// Scratch is the optional node-local temporary byte journal reserved by
	// this component's Compile result. It is borrowed for Open-to-Close only.
	Scratch Scratch
}

// NewOpenContext snapshots the narrow services granted to one component
// instance. Host runtime is its production consumer; standalone conformance
// tests can construct the same boundary without a service locator.
func NewOpenContext(ctx context.Context, services OpenServices) OpenContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return OpenContext{
		context:     ctx,
		buffers:     services.Buffers,
		tasks:       services.Tasks,
		diagnostics: services.Diagnostics,
		owner:       services.Owner,
		boundary:    services.Boundary,
		source:      services.Source,
		scratch:     services.Scratch,
	}
}

type OpenContext struct {
	context     context.Context
	buffers     *buffer.Allocator
	tasks       TaskStarter
	diagnostics diagnostic.Sink
	owner       flow.Owner
	boundary    any
	source      any
	scratch     Scratch
}

func (c OpenContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c OpenContext) Buffers() *buffer.Allocator   { return c.buffers }
func (c OpenContext) Diagnostics() diagnostic.Sink { return c.diagnostics }

// Tasks returns the node-local task starter. The component's Compile result
// must declare resource.Request{Workers: N} before Open starts up to N
// concurrent tasks; otherwise Start returns ErrWorkerLimit.
func (c OpenContext) Tasks() TaskStarter { return c.tasks }

// Owner returns this component's own failure domain.
//
// Slots filled through Emitter.Own already report somewhere that lives as long
// as the run, so an ordinary component never needs this. A component that
// keeps a payload past the call it arrived in -- moving an input into a slot
// of its own and releasing it during Flush or Close -- binds that slot here.
// That is what makes the slot's lifetime its own declaration rather than an
// accident of which caller handed the payload over, and it is why an unbound
// slot refuses ownership instead of inheriting the sender's domain.
//
//	// during Open
//	p.held.Bind(payloadType, ctx.Owner())
//
//	// during Process
//	p.held.Move(input)
//
//	// during Flush or Close
//	p.held.Drop()
func (c OpenContext) Owner() flow.Owner { return c.owner }

// Boundary recovers the one typed Access/Endpoint binding attached to this
// node. It is a control-plane assertion performed once during Open; media
// items never cross this erased boundary.
func Boundary[T any](c OpenContext) (T, bool) {
	value, ok := c.boundary.(T)
	return value, ok
}

// Source recovers the optional typed read-only source view associated with
// prepared component state. The Host owns its lifetime; an operator must not
// use it after Close.
func Source[T any](c OpenContext) (T, bool) {
	value, ok := c.source.(T)
	return value, ok
}

// Scratch returns the node-local, Host-owned temporary byte journal. It is nil
// unless this component declared a positive scratch claim during Compile.
func (c OpenContext) Scratch() Scratch { return c.scratch }

type CompileFunc[C, P, D any] func(CompileContext, C, flow.Descriptors[D]) (Compiled[P, D], error)
type SuggestFunc[C, D any] func(SuggestContext, Suggestion[D]) []C
type OpenFunc[P any] func(OpenContext, P) (flow.Operator, error)

// Spec is the complete semantic contract for one component implementation.
// D is the control-plane descriptor type; normal media components use
// stream.Descriptor without making flow or plugin import media/stream.
type Spec[C, P, D any] struct {
	Ports           flow.Shape
	Compile         CompileFunc[C, P, D]
	Suggest         SuggestFunc[C, D]
	Open            OpenFunc[P]
	SuggestionLimit int
	Contract        Contract
}

// Compiled is the typed result plugin authors return. P remains private inside
// Compilation; Host only exposes its descriptor/effect/resource projection.
type Compiled[P, D any] struct {
	Plan         P
	Outputs      flow.Descriptors[D]
	Requirements []Requirement[D]
	Effects      []Effect
	Resources    resource.Request
	Scratch      resource.Bytes
	Estimate     resource.Estimate
}

type componentImplementation struct {
	ports           flow.Shape
	compile         func(CompileContext, config.ResolvedView, any) (compiledErased, error)
	suggest         func(SuggestContext, any) ([]any, error)
	open            func(OpenContext, any) (flow.Operator, error)
	suggestionLimit int
	finalizes       bool
	contract        Contract
	problems        []diagnostic.Item
}

type compiledErased struct {
	plan         any
	outputs      any
	requirements any
	effects      []Effect
	resources    resource.Request
	scratch      resource.Bytes
	estimate     resource.Estimate
}

// WithSpec type-erases one typed Spec at component construction time.
func WithSpec[C, P, D any](spec Spec[C, P, D]) ComponentOption {
	implementation := &componentImplementation{
		suggestionLimit: spec.SuggestionLimit,
		contract:        normalizeContract(spec.Contract),
		ports:           spec.Ports.Clone(),
	}
	if !implementation.contract.Valid() {
		implementation.problems = append(implementation.problems, specItem("plugin.contract", "component Spec has an invalid implementation contract"))
	}
	if err := spec.Ports.Validate(); err != nil {
		implementation.problems = append(implementation.problems, specItem("plugin.ports", "component Spec requires valid Ports: "+err.Error()))
	}
	if spec.Compile == nil {
		implementation.problems = append(implementation.problems, specItem("plugin.compile", "component Spec requires Compile"))
	} else {
		implementation.compile = func(ctx CompileContext, resolved config.ResolvedView, inputs any) (compiledErased, error) {
			value, err := typedConfig[C](resolved)
			if err != nil {
				return compiledErased{}, err
			}
			typedInputs, ok := inputs.(flow.Descriptors[D])
			if !ok {
				return compiledErased{}, errors.New("input descriptor type does not match component Spec")
			}
			compiled, err := spec.Compile(ctx, value, typedInputs)
			return compiledErased{
				plan:         compiled.Plan,
				outputs:      compiled.Outputs,
				requirements: append([]Requirement[D](nil), compiled.Requirements...),
				effects:      append([]Effect(nil), compiled.Effects...),
				resources:    compiled.Resources,
				scratch:      compiled.Scratch,
				estimate:     compiled.Estimate,
			}, err
		}
	}
	if spec.Open == nil {
		implementation.problems = append(implementation.problems, specItem("plugin.open", "component Spec requires Open"))
	} else {
		implementation.open = func(ctx OpenContext, plan any) (flow.Operator, error) {
			typed, ok := plan.(P)
			if !ok {
				return nil, errors.New("compiled plan has the wrong type for component Open")
			}
			return spec.Open(ctx, typed)
		}
	}
	if spec.Suggest == nil {
		if spec.SuggestionLimit != 0 {
			implementation.problems = append(implementation.problems, specItem("plugin.suggest", "SuggestionLimit requires Suggest"))
		}
	} else {
		if spec.SuggestionLimit <= 0 {
			implementation.problems = append(implementation.problems, specItem("plugin.suggest", "Suggest requires a positive SuggestionLimit"))
		}
		implementation.suggest = func(ctx SuggestContext, suggestion any) ([]any, error) {
			typedSuggestion, ok := suggestion.(Suggestion[D])
			if !ok {
				return nil, errors.New("suggestion descriptor type does not match component Suggest")
			}
			values := spec.Suggest(ctx, cloneSuggestion(typedSuggestion))
			result := make([]any, len(values))
			for index := range values {
				result[index] = values[index]
			}
			return result, nil
		}
	}
	return func(options *componentOptions) {
		if options.implementation != nil {
			options.problems = append(options.problems, specItem("plugin.spec", "component may declare only one Spec"))
			return
		}
		options.implementation = implementation
	}
}

func specItem(code, message string) diagnostic.Item {
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{}, message, nil)
}

// typedConfig takes the phase's own snapshot of a resolved config. A failed
// snapshot is an error rather than a fallback value: the alternative is a
// config that still aliases what the previous phase saw.
func typedConfig[C any](resolved config.ResolvedView) (C, error) {
	var zero C
	snapshot, err := resolved.Value()
	if err != nil {
		return zero, err
	}
	value, ok := snapshot.(C)
	if !ok {
		return zero, errors.New("resolved config has the wrong type for this component")
	}
	return value, nil
}
