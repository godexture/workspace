package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/resource"
)

var (
	ErrComponentSpec        = errors.New("component has no Spec")
	ErrCompilationComponent = errors.New("compilation belongs to a different component implementation")
)

// Compilation is the immutable, type-erased Host view of one Compile result.
// Its private plan can only be consumed by Component.Open.
type Compilation struct {
	component      Identity
	implementation *componentImplementation
	config         config.Fingerprint
	plan           any
	outputs        any
	requirements   any
	effects        []Effect
	resources      resource.Request
	estimate       resource.Estimate
	finalization   Finalization
}

func (c Compilation) Valid() bool {
	return !c.component.IsZero() && c.implementation != nil && !c.config.IsZero() && c.outputs != nil && c.requirements != nil
}

func (c Compilation) Component() Identity                   { return c.component }
func (c Compilation) ConfigFingerprint() config.Fingerprint { return c.config }
func (c Compilation) Effects() []Effect                     { return append([]Effect(nil), c.effects...) }
func (c Compilation) Resources() resource.Request           { return c.resources }
func (c Compilation) Estimate() resource.Estimate           { return c.estimate }
func (c Compilation) Finalization() Finalization            { return c.finalization }

func OutputsOf[D any](compilation Compilation) (flow.Descriptors[D], bool) {
	value, ok := compilation.outputs.(flow.Descriptors[D])
	return value, ok
}

func RequirementsOf[D any](compilation Compilation) ([]Requirement[D], bool) {
	values, ok := compilation.requirements.([]Requirement[D])
	return append([]Requirement[D](nil), values...), ok
}

// Shape resolves the component port topology from a validated config.
func (c Component) Shape(ctx ShapeContext, resolved config.ResolvedView) (shape flow.Shape, err error) {
	if c.implementation == nil || c.implementation.shape == nil {
		return flow.Shape{}, ErrComponentSpec
	}
	if err := c.validateResolved(resolved); err != nil {
		return flow.Shape{}, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = c.phaseError("plugin.shape-panic", "component Shape panicked", fmt.Sprint(recovered))
			shape = flow.Shape{}
		}
	}()
	shape, err = c.implementation.shape(ctx, resolved)
	if err != nil {
		return flow.Shape{}, c.phaseError("plugin.shape", "component Shape failed", err.Error())
	}
	if err := shape.Validate(); err != nil {
		return flow.Shape{}, c.phaseError("plugin.port-shape", "component Shape returned invalid ports", err.Error())
	}
	return shape.Clone(), nil
}

// Compile invokes the component's pure semantic transformation. D remains
// typed for plugin and Host code even though Component itself is heterogeneous.
func Compile[D any](component Component, ctx CompileContext, resolved config.ResolvedView, inputs flow.Descriptors[D]) (compilation Compilation, err error) {
	if component.implementation == nil || component.implementation.compile == nil {
		return Compilation{}, ErrComponentSpec
	}
	if err := component.validateResolved(resolved); err != nil {
		return Compilation{}, err
	}
	shape, err := component.Shape(ShapeContext{}, resolved)
	if err != nil {
		return Compilation{}, err
	}
	if items := validateDescriptorPorts("input", shape.Inputs, inputs.Bindings()); len(items) != 0 {
		return Compilation{}, diagnostic.NewError(prefixComponent(items, component.identity)...)
	}

	var compiled compiledErased
	defer func() {
		if recovered := recover(); recovered != nil {
			err = component.phaseError("plugin.compile-panic", "component Compile panicked", fmt.Sprint(recovered))
			compilation = Compilation{}
		}
	}()
	compiled, err = component.implementation.compile(ctx, resolved, inputs)
	if err != nil {
		return Compilation{}, component.phaseError("plugin.compile", "component Compile failed", err.Error())
	}
	outputs, ok := compiled.outputs.(flow.Descriptors[D])
	if !ok {
		return Compilation{}, component.phaseError("plugin.compile-output", "component Compile returned the wrong descriptor type", "")
	}
	requirements, ok := compiled.requirements.([]Requirement[D])
	if !ok {
		return Compilation{}, component.phaseError("plugin.compile-requirement", "component Compile returned the wrong requirement type", "")
	}
	items := validateDescriptorPorts("output", shape.Outputs, outputs.Bindings())
	for _, requirement := range requirements {
		if !requirement.Valid() || !shapeHasPort(shape.Inputs, requirement.Port()) {
			items = append(items, diagnostic.NewItem("plugin.compile-requirement", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: requirement.Port()}, "component Compile returned an invalid input requirement", nil))
		}
	}
	for _, effect := range compiled.effects {
		if !effect.Valid() {
			items = append(items, diagnostic.NewItem("plugin.compile-effect", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned an invalid effect", nil))
		}
	}
	if !compiled.estimate.Valid() {
		items = append(items, diagnostic.NewItem("plugin.compile-estimate", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned an invalid resource estimate", nil))
	}
	if !compiled.finalization.Valid() {
		items = append(items, diagnostic.NewItem("plugin.compile-finalization", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned an invalid finalization requirement", nil))
	}
	if compiled.finalization == RequiresFinalization && !component.implementation.finalizes {
		items = append(items, diagnostic.NewItem("plugin.finalizer", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile requires finalization but Spec has no finalizer capability", nil))
	}
	if len(items) != 0 {
		return Compilation{}, diagnostic.NewError(prefixComponent(items, component.identity)...)
	}
	return Compilation{
		component:      component.identity,
		implementation: component.implementation,
		config:         resolved.Fingerprint,
		plan:           compiled.plan,
		outputs:        outputs,
		requirements:   append([]Requirement[D](nil), requirements...),
		effects:        append([]Effect(nil), compiled.effects...),
		resources:      compiled.resources,
		estimate:       compiled.estimate,
		finalization:   compiled.finalization,
	}, nil
}

// Suggest returns validated, canonical config candidates. It never calls
// Compile or Open; the planner evaluates each returned config with Compile.
func Suggest[D any](component Component, ctx SuggestContext, input D, need Need[D]) (candidates []config.ResolvedView, err error) {
	implementation := component.implementation
	if implementation == nil || implementation.suggest == nil {
		return nil, nil
	}
	if !need.Valid() {
		return nil, component.phaseError("plugin.suggest-need", "component Suggest received an invalid need", "")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = component.phaseError("plugin.suggest-panic", "component Suggest panicked", fmt.Sprint(recovered))
			candidates = nil
		}
	}()
	values, err := implementation.suggest(ctx, input, need)
	if err != nil {
		return nil, component.phaseError("plugin.suggest", "component Suggest failed", err.Error())
	}
	if len(values) > implementation.suggestionLimit {
		return nil, component.phaseError("plugin.suggest-limit", "component Suggest exceeded its declared limit", fmt.Sprintf("returned=%d limit=%d", len(values), implementation.suggestionLimit))
	}
	seen := make(map[config.Fingerprint]struct{}, len(values))
	for _, value := range values {
		resolved, err := component.schema.ResolveValue(value)
		if err != nil {
			return nil, component.phaseError("plugin.suggest-config", "component Suggest returned an invalid config", err.Error())
		}
		if _, exists := seen[resolved.Fingerprint]; exists {
			return nil, component.phaseError("plugin.suggest-duplicate", "component Suggest returned duplicate canonical config", resolved.Fingerprint.String())
		}
		seen[resolved.Fingerprint] = struct{}{}
		candidates = append(candidates, resolved)
	}
	return candidates, nil
}

// Open consumes only a Compilation created by this exact component Spec.
func (c Component) Open(ctx context.Context, compilation Compilation) (operator flow.Operator, err error) {
	if c.implementation == nil || c.implementation.open == nil {
		return nil, ErrComponentSpec
	}
	if compilation.component != c.identity || compilation.implementation != c.implementation {
		return nil, ErrCompilationComponent
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = c.phaseError("plugin.open-panic", "component Open panicked", fmt.Sprint(recovered))
			operator = nil
		}
	}()
	operator, err = c.implementation.open(newOpenContext(ctx), compilation.plan)
	if err != nil {
		return nil, c.phaseError("plugin.open", "component Open failed", err.Error())
	}
	if operator == nil {
		return nil, c.phaseError("plugin.open", "component Open returned a nil operator", "")
	}
	return operator, nil
}

func (c Component) validateResolved(resolved config.ResolvedView) error {
	identity := c.schema.Description().Identity
	if resolved.Schema != identity || resolved.Fingerprint.IsZero() {
		return c.phaseError("plugin.config", "resolved config does not belong to this component schema", "")
	}
	return nil
}

func (c Component) phaseError(code, message, detail string) error {
	metadata := map[string]string(nil)
	if detail != "" {
		metadata = map[string]string{"cause": detail}
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: c.identity.String()}, message, metadata))
}

func validateDescriptorPorts[D any](phase string, ports []flow.Port, bindings []flow.PortDescriptor[D]) []diagnostic.Item {
	portByID := make(map[string]flow.Port, len(ports))
	counts := make(map[string]int, len(ports))
	for _, port := range ports {
		portByID[port.ID()] = port
	}
	var items []diagnostic.Item
	for _, binding := range bindings {
		port, exists := portByID[binding.Port()]
		if !binding.Valid() || !exists {
			items = append(items, diagnostic.NewItem("plugin.compile-"+phase+"-port", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: binding.Port()}, "descriptor binding names an unknown or invalid "+phase+" port", nil))
			continue
		}
		counts[binding.Port()]++
		if counts[binding.Port()] > 1 && port.Multiplicity() != flow.ManyMultiplicity {
			items = append(items, diagnostic.NewItem("plugin.compile-"+phase+"-multiplicity", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: binding.Port()}, "descriptor binding exceeds port multiplicity", nil))
		}
	}
	return items
}

func shapeHasPort(ports []flow.Port, id string) bool {
	for _, port := range ports {
		if port.ID() == id {
			return true
		}
	}
	return false
}

func prefixComponent(items []diagnostic.Item, identity Identity) []diagnostic.Item {
	result := make([]diagnostic.Item, len(items))
	for index, item := range items {
		result[index] = item.WithPath(item.Path.WithComponent(identity.String()))
	}
	return result
}
