package plugin

import (
	"context"
	"errors"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/resource"
)

type ShapeContext struct{}
type CompileContext struct{}
type SuggestContext struct{}

type OpenContext struct{ context context.Context }

func newOpenContext(ctx context.Context) OpenContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return OpenContext{context: ctx}
}

func (c OpenContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
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
	}
	if spec.Shape == nil {
		implementation.problems = append(implementation.problems, specItem("plugin.shape", "component Spec requires Shape"))
	} else {
		implementation.shape = func(ctx ShapeContext, resolved config.ResolvedView) (flow.Shape, error) {
			value, ok := resolved.Value.(C)
			if !ok {
				return flow.Shape{}, errors.New("resolved config has the wrong type for component Shape")
			}
			return spec.Shape(ctx, value)
		}
	}
	if spec.Compile == nil {
		implementation.problems = append(implementation.problems, specItem("plugin.compile", "component Spec requires Compile"))
	} else {
		implementation.compile = func(ctx CompileContext, resolved config.ResolvedView, inputs any) (compiledErased, error) {
			value, ok := resolved.Value.(C)
			if !ok {
				return compiledErased{}, errors.New("resolved config has the wrong type for component Compile")
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
