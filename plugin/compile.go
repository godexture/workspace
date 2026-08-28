package plugin

import (
	"errors"
	"fmt"
	"math"
	"runtime/debug"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/resource"
)

var (
	ErrComponentSpec        = errors.New("component has no Spec")
	ErrCompilationComponent = errors.New("compilation belongs to a different component implementation")
)

// Compilation is the immutable, type-erased Host view of one Compile result.
// Its private plan can only be consumed by Component.Open.
type Compilation struct {
	component       Identity
	implementation  *componentImplementation
	config          config.Fingerprint
	shape           flow.Shape
	plan            any
	outputs         any
	requirements    any
	effects         []Effect
	metadataReports []MetadataReport
	resources       resource.Request
	scratch         resource.Bytes
	temporary       resource.Bytes
	estimate        resource.Estimate
	execution       drive.Binding
	executionSet    bool
}

func (c Compilation) Valid() bool {
	return !c.component.IsZero() && c.implementation != nil && !c.config.IsZero() && !c.shape.Empty() && c.outputs != nil && c.requirements != nil
}

func (c Compilation) Component() Identity                   { return c.component }
func (c Compilation) ConfigFingerprint() config.Fingerprint { return c.config }
func (c Compilation) Effects() []Effect                     { return append([]Effect(nil), c.effects...) }
func (c Compilation) MetadataReports() []MetadataReport {
	return append([]MetadataReport(nil), c.metadataReports...)
}
func (c Compilation) Resources() resource.Request { return c.resources }
func (c Compilation) Scratch() resource.Bytes     { return c.scratch }
func (c Compilation) Temporary() resource.Bytes   { return c.temporary }
func (c Compilation) Estimate() resource.Estimate { return c.estimate }

func OutputsOf[D any](compilation Compilation) (flow.Descriptors[D], bool) {
	value, ok := compilation.outputs.(flow.Descriptors[D])
	return value, ok
}

func RequirementsOf[D any](compilation Compilation) ([]Requirement[D], bool) {
	values, ok := compilation.requirements.([]Requirement[D])
	return append([]Requirement[D](nil), values...), ok
}

func (c Component) staticPorts() (flow.Shape, error) {
	if c.implementation == nil {
		return flow.Shape{}, ErrComponentSpec
	}
	shape := c.Ports()
	if err := shape.Validate(); err != nil {
		return flow.Shape{}, c.phaseError("plugin.ports", "component Spec has invalid Ports", err.Error())
	}
	return shape, nil
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
	shape, err := component.staticPorts()
	if err != nil {
		return Compilation{}, err
	}
	if items := validateDescriptorPorts("plugin.compile", "input", shape.Inputs, inputs.Bindings()); len(items) != 0 {
		return Compilation{}, diagnostic.NewError(prefixComponent(items, component.identity)...)
	}

	var compiled compiledErased
	defer func() {
		if recovered := recover(); recovered != nil {
			err = component.panicError("plugin.compile-panic", "component Compile panicked", recovered)
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
	items := validateDescriptorPorts("plugin.compile", "output", shape.Outputs, outputs.Bindings())
	outputCounts := descriptorCounts(outputs.Bindings())
	inputCounts := descriptorCounts(inputs.Bindings())
	requirementPorts := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if !requirement.Valid() || !shapeHasPort(shape.Inputs, requirement.Port()) {
			items = append(items, diagnostic.NewItem("plugin.compile-requirement", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: requirement.Port()}, "component Compile returned an invalid input requirement", nil))
		}
		if _, exists := requirementPorts[requirement.Port()]; exists {
			items = append(items, diagnostic.NewItem("plugin.compile-duplicate-requirement", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: requirement.Port()}, "component Compile returned more than one requirement for an input port", nil))
		}
		requirementPorts[requirement.Port()] = struct{}{}
	}
	for _, port := range shape.Inputs {
		if !port.Required() || inputCounts[port.ID()] != 0 {
			continue
		}
		if _, exists := requirementPorts[port.ID()]; !exists {
			items = append(items, diagnostic.NewItem("plugin.compile-missing-requirement", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: port.ID()}, "component Compile accepted a missing required input without returning a requirement", nil))
		}
	}
	if len(requirements) == 0 {
		for _, port := range shape.Outputs {
			if port.Required() && outputCounts[port.ID()] == 0 {
				items = append(items, diagnostic.NewItem("plugin.compile-required-output", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: port.ID()}, "component Compile omitted a required output descriptor", nil))
			}
		}
	}
	for _, effect := range compiled.effects {
		if !effect.Valid() {
			items = append(items, diagnostic.NewItem("plugin.compile-effect", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned an invalid effect", nil))
		}
	}
	for _, value := range compiled.metadataReports {
		if !value.valid() || !shapeHasPort(shape.Outputs, value.Output) || outputCounts[value.Output] == 0 {
			items = append(items, diagnostic.NewItem("plugin.compile-metadata-report", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: value.Output}, "component Compile returned metadata evidence without a valid output port", nil))
		}
	}
	if !compiled.estimate.Valid() {
		items = append(items, diagnostic.NewItem("plugin.compile-estimate", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned an invalid resource estimate", nil))
	}
	if uint64(compiled.scratch) > math.MaxInt64 {
		items = append(items, diagnostic.NewItem("plugin.compile-scratch", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned a scratch claim outside the runtime range", nil))
	}
	if uint64(compiled.temporary) > math.MaxInt64 {
		items = append(items, diagnostic.NewItem("plugin.compile-temporary", diagnostic.ErrorSeverity, diagnostic.Path{}, "component Compile returned a temporary claim outside the runtime range", nil))
	}
	if len(items) != 0 {
		return Compilation{}, diagnostic.NewError(prefixComponent(items, component.identity)...)
	}
	return Compilation{
		component:       component.identity,
		implementation:  component.implementation,
		config:          resolved.Fingerprint(),
		shape:           shape.Clone(),
		plan:            compiled.plan,
		outputs:         outputs,
		requirements:    append([]Requirement[D](nil), requirements...),
		effects:         append([]Effect(nil), compiled.effects...),
		metadataReports: append([]MetadataReport(nil), compiled.metadataReports...),
		resources:       compiled.resources,
		scratch:         compiled.scratch,
		temporary:       compiled.temporary,
		estimate:        compiled.estimate,
		execution:       component.execution,
		executionSet:    component.executionSet,
	}, nil
}

// Suggest returns validated, canonical config candidates. It never calls
// Compile or Open; the planner evaluates each returned config with Compile.
func Suggest[D any](component Component, ctx SuggestContext, suggestion Suggestion[D]) (candidates []config.ResolvedView, err error) {
	implementation := component.implementation
	if implementation == nil || implementation.suggest == nil {
		return nil, nil
	}
	shape, err := component.staticPorts()
	if err != nil {
		return nil, err
	}
	items := validateDescriptorPorts("plugin.suggest", "input", shape.Inputs, suggestion.Inputs().Bindings())
	items = append(items, validateSuggestionDemands(shape, suggestion.Demands())...)
	if len(items) != 0 {
		return nil, diagnostic.NewError(prefixComponent(items, component.identity)...)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = component.panicError("plugin.suggest-panic", "component Suggest panicked", recovered)
			candidates = nil
		}
	}()
	values, err := implementation.suggest(ctx, cloneSuggestion(suggestion))
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
		if _, exists := seen[resolved.Fingerprint()]; exists {
			return nil, component.phaseError("plugin.suggest-duplicate", "component Suggest returned duplicate canonical config", resolved.Fingerprint().String())
		}
		seen[resolved.Fingerprint()] = struct{}{}
		candidates = append(candidates, resolved)
	}
	return candidates, nil
}

// Open consumes only a Compilation created by this exact component Spec.
func (c Component) Open(ctx OpenContext, compilation Compilation) (operator flow.Operator, err error) {
	if c.implementation == nil || c.implementation.open == nil {
		return nil, ErrComponentSpec
	}
	if compilation.component != c.identity || compilation.implementation != c.implementation {
		return nil, ErrCompilationComponent
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = c.panicError("plugin.open-panic", "component Open panicked", recovered)
			operator = nil
		}
	}()
	operator, err = c.implementation.open(ctx, compilation.plan)
	if err != nil {
		return nil, c.phaseError("plugin.open", "component Open failed", err.Error())
	}
	if operator == nil {
		return nil, c.phaseError("plugin.open", "component Open returned a nil operator", "")
	}
	if openedShape := operator.Ports(); !openedShape.Equal(compilation.shape) {
		detail := "operator ports differ from the compiled Ports"
		if closeErr := operator.Close(); closeErr != nil {
			detail += ": close failed: " + closeErr.Error()
		}
		return nil, c.phaseError("plugin.open-shape", "component Open returned an incompatible port shape", detail)
	}
	if compilation.executionSet {
		if err := compilation.execution.ValidateOperator(operator); err != nil {
			detail := err.Error()
			if closeErr := operator.Close(); closeErr != nil {
				detail += ": close failed: " + closeErr.Error()
			}
			return nil, c.phaseError("plugin.open-execution", "component Open returned an incompatible typed operator", detail)
		}
	}
	return operator, nil
}

func (c Component) validateResolved(resolved config.ResolvedView) error {
	identity := c.schema.Description().Identity
	if resolved.Schema() != identity || resolved.Fingerprint().IsZero() {
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

func (c Component) panicError(code, message string, recovered any) error {
	return diagnostic.NewError(diagnostic.NewItem(
		code,
		diagnostic.ErrorSeverity,
		diagnostic.Path{Component: c.identity.String()},
		message,
		map[string]string{"cause": diagnostic.Recovered(recovered), "stack": string(debug.Stack())},
	))
}

func validateDescriptorPorts[D any](prefix, phase string, ports []flow.Port, bindings []flow.PortDescriptor[D]) []diagnostic.Item {
	portByID := make(map[string]flow.Port, len(ports))
	counts := make(map[string]int, len(ports))
	for _, port := range ports {
		portByID[port.ID()] = port
	}
	var items []diagnostic.Item
	for _, binding := range bindings {
		port, exists := portByID[binding.Port()]
		if !binding.Valid() || !exists {
			items = append(items, diagnostic.NewItem(prefix+"-"+phase+"-port", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: binding.Port()}, "descriptor binding names an unknown or invalid "+phase+" port", nil))
			continue
		}
		counts[binding.Port()]++
		if counts[binding.Port()] > 1 && port.Multiplicity() != flow.ManyMultiplicity {
			items = append(items, diagnostic.NewItem(prefix+"-"+phase+"-multiplicity", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: binding.Port()}, "descriptor binding exceeds port multiplicity", nil))
		}
	}
	return items
}

func validateSuggestionDemands[D any](shape flow.Shape, demands []Demand[D]) []diagnostic.Item {
	portByDirection := map[flow.Direction]map[string]struct{}{
		flow.InputDirection:  make(map[string]struct{}, len(shape.Inputs)),
		flow.OutputDirection: make(map[string]struct{}, len(shape.Outputs)),
	}
	for _, port := range shape.Inputs {
		portByDirection[flow.InputDirection][port.ID()] = struct{}{}
	}
	for _, port := range shape.Outputs {
		portByDirection[flow.OutputDirection][port.ID()] = struct{}{}
	}
	var items []diagnostic.Item
	for _, demand := range demands {
		path := diagnostic.Path{Descriptor: demand.Port()}
		if !demand.Need().Valid() {
			items = append(items, diagnostic.NewItem("plugin.suggest-need", diagnostic.ErrorSeverity, path, "component Suggest received an invalid need", nil))
			continue
		}
		if !demand.Valid() {
			items = append(items, diagnostic.NewItem("plugin.suggest-demand", diagnostic.ErrorSeverity, path, "component Suggest received an invalid demand", nil))
			continue
		}
		if _, exists := portByDirection[demand.Direction()][demand.Port()]; !exists {
			items = append(items, diagnostic.NewItem("plugin.suggest-demand-port", diagnostic.ErrorSeverity, path, "component Suggest demand names an unknown or incompatible port", nil))
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

func descriptorCounts[D any](bindings []flow.PortDescriptor[D]) map[string]int {
	counts := make(map[string]int, len(bindings))
	for _, binding := range bindings {
		counts[binding.Port()]++
	}
	return counts
}

func prefixComponent(items []diagnostic.Item, identity Identity) []diagnostic.Item {
	result := make([]diagnostic.Item, len(items))
	for index, item := range items {
		result[index] = item.WithPath(item.Path.WithComponent(identity.String()))
	}
	return result
}
