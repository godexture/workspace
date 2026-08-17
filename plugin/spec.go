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

type ShapeContext struct{}
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
	}
}

type OpenContext struct {
	context     context.Context
	buffers     *buffer.Allocator
	tasks       TaskStarter
	diagnostics diagnostic.Sink
	owner       flow.Owner
	boundary    any
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

type ShapeFunc[C any] func(ShapeContext, C) (flow.Shape, error)
type CompileFunc[C, P, D any] func(CompileContext, C, flow.Descriptors[D]) (Compiled[P, D], error)
type SuggestFunc[C, D any] func(SuggestContext, D, Need[D]) []C
type OpenFunc[P any] func(OpenContext, P) (flow.Operator, error)

// StaticShape adapts a fixed shape to the same phase used by dynamic
// components.
func StaticShape[C any](shape flow.Shape) ShapeFunc[C] {
	shape = shape.Clone()
	return func(ShapeContext, C) (flow.Shape, error) { return shape.Clone(), nil }
}

// Spec is the complete semantic contract for one component implementation.
// D is the control-plane descriptor type; normal media components use
// stream.Descriptor without making flow or plugin import media/stream.
type Spec[C, P, D any] struct {
	Shape           ShapeFunc[C]
	Compile         CompileFunc[C, P, D]
	Suggest         SuggestFunc[C, D]
	Open            OpenFunc[P]
	SuggestionLimit int
	DynamicShape    bool
	Finalizes       bool
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
	Estimate     resource.Estimate
	Finalization Finalization
}

type componentImplementation struct {
	shape           func(ShapeContext, config.ResolvedView) (flow.Shape, error)
	compile         func(CompileContext, config.ResolvedView, any) (compiledErased, error)
	suggest         func(SuggestContext, any, any) ([]any, error)
	open            func(OpenContext, any) (flow.Operator, error)
	suggestionLimit int
	dynamicShape    bool
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
	estimate     resource.Estimate
	finalization Finalization
}

// WithSpec type-erases one typed Spec at component construction time.
func WithSpec[C, P, D any](spec Spec[C, P, D]) ComponentOption {
	implementation := &componentImplementation{
		suggestionLimit: spec.SuggestionLimit,
		dynamicShape:    spec.DynamicShape,
		finalizes:       spec.Finalizes,
		contract:        normalizeContract(spec.Contract),
	}
	if !implementation.contract.Valid() {
		implementation.problems = append(implementation.problems, specItem("plugin.contract", "component Spec has an invalid implementation contract"))
	}
	if spec.Shape == nil {
		implementation.problems = append(implementation.problems, specItem("plugin.shape", "component Spec requires Shape"))
	} else {
		implementation.shape = func(ctx ShapeContext, resolved config.ResolvedView) (flow.Shape, error) {
			value, err := typedConfig[C](resolved)
			if err != nil {
				return flow.Shape{}, err
			}
			return spec.Shape(ctx, value)
		}
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
				estimate:     compiled.Estimate,
				finalization: compiled.Finalization,
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
		implementation.suggest = func(ctx SuggestContext, input, need any) ([]any, error) {
			typedInput, ok := input.(D)
			if !ok {
				return nil, errors.New("input descriptor type does not match component Suggest")
			}
			typedNeed, ok := need.(Need[D])
			if !ok {
				return nil, errors.New("need descriptor type does not match component Suggest")
			}
			values := spec.Suggest(ctx, typedInput, typedNeed)
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
