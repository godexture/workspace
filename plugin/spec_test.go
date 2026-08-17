package plugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type specUnitID struct{}
type specOtherID struct{}
type specTraitID struct{}
type specUnit struct{ Value int }
type specPlan struct {
	shape flow.Shape
	value int
}

type specOperator struct{ shape flow.Shape }

func (o specOperator) Ports() flow.Shape { return o.shape }
func (o specOperator) Close() error      { return nil }

type specProcessorOperator struct{ specOperator }

func (specProcessorOperator) Process(context.Context, *flow.Item[specUnit], flow.Emitter[specUnit]) error {
	return nil
}

func (specProcessorOperator) Flush(context.Context, flow.Emitter[specUnit]) error { return nil }

type specRouterOperator struct{ specOperator }

func (specRouterOperator) Process(context.Context, *flow.Item[specUnit], flow.RoutedEmitter[specUnit]) error {
	return nil
}

func (specRouterOperator) Flush(context.Context, flow.RoutedEmitter[specUnit]) error { return nil }

type specFinalizerOperator struct{ specOperator }

func (specFinalizerOperator) Finalize(context.Context) error { return nil }

type trackedSpecOperator struct {
	shape  flow.Shape
	closed *atomic.Int32
}

func (o trackedSpecOperator) Ports() flow.Shape { return o.shape }
func (o trackedSpecOperator) Close() error {
	o.closed.Add(1)
	return nil
}

func testSpec(opened *atomic.Int32, suggest SuggestFunc[pluginConfig, int]) Spec[pluginConfig, specPlan, int] {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	limit := 0
	if suggest != nil {
		limit = 3
	}
	return Spec[pluginConfig, specPlan, int]{
		Ports: shape,
		Compile: func(_ CompileContext, value pluginConfig, inputs flow.Descriptors[int]) (Compiled[specPlan, int], error) {
			input, ok := inputs.One("in")
			if !ok {
				return Compiled[specPlan, int]{Requirements: []Requirement[int]{Require("in", ConditionNeed[int]("fixture.input"))}}, nil
			}
			return Compiled[specPlan, int]{
				Plan:    specPlan{shape: shape, value: input + value.Level},
				Outputs: flow.NewDescriptors(flow.Describe("out", input+value.Level)),
				Effects: []Effect{{Kind: RepresentationEffect, Loss: NoLoss, Detail: "fixture.offset"}},
			}, nil
		},
		Suggest:         suggest,
		SuggestionLimit: limit,
		Open: func(_ OpenContext, plan specPlan) (flow.Operator, error) {
			if opened != nil {
				opened.Add(1)
			}
			return specOperator{shape: plan.shape}, nil
		},
	}
}

func TestComponentSpecShapesCompilesAndOpensSelectedPlan(t *testing.T) {
	var opened atomic.Int32
	component := NewComponent[specUnitID](Descriptor{DisplayName: "spec"}, pluginSchema(1), WithSpec(testSpec(&opened, nil)))
	if diagnostics := component.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("component diagnostics = %v", diagnostics)
	}
	resolved, err := component.Resolve(componentPatch(t, component, "level", 2))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 5)))
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.Valid() {
		t.Fatal("compilation is invalid")
	}
	outputs, ok := OutputsOf[int](compiled)
	value, one := outputs.One("out")
	if !ok || !one || value != 7 {
		t.Fatalf("compiled outputs = %#v", outputs.Bindings())
	}
	if opened.Load() != 0 {
		t.Fatal("Compile opened an operator")
	}
	operator, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 1 {
		t.Fatalf("Open count = %d", opened.Load())
	}
	if err := operator.Ports().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionBindingValidatesShapeAndOpenedTypedOperator(t *testing.T) {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	spec := testSpec(nil, nil)
	spec.Open = func(_ OpenContext, plan specPlan) (flow.Operator, error) {
		return specProcessorOperator{specOperator{shape: plan.shape}}, nil
	}
	component := NewComponent[specUnitID](
		Descriptor{DisplayName: "typed execution"},
		pluginSchema(1),
		WithSpec(spec),
		WithProcessor("in", typ, "out", typ),
	)
	if diagnostics := component.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("component diagnostics = %v", diagnostics)
	}
	if !component.View().Executable {
		t.Fatal("component view did not expose its executable binding")
	}
	resolved, _ := component.Resolve(config.NewPatch())
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ExecutionOf(compiled); !ok {
		t.Fatal("compilation lost its typed execution binding")
	}
	operator, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	_ = operator.Close()

	wrong := testSpec(nil, nil)
	component = NewComponent[specOtherID](
		Descriptor{DisplayName: "wrong typed execution"},
		pluginSchema(1),
		WithSpec(wrong),
		WithProcessor("in", typ, "out", typ),
	)
	resolved, _ = component.Resolve(config.NewPatch())
	compiled, err = Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled); !hasDiagnostic(err, "plugin.open-execution") {
		t.Fatalf("typed operator error = %v", err)
	}
}

func TestExecutionBindingRejectsDuplicateAndShapeMismatch(t *testing.T) {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	duplicate := NewComponent[specUnitID](
		Descriptor{DisplayName: "duplicate execution"},
		pluginSchema(1),
		WithSpec(testSpec(nil, nil)),
		WithProcessor("in", typ, "out", typ),
		WithProcessor("in", typ, "out", typ),
	)
	if !hasItem(duplicate.Diagnostics(), "plugin.execution") {
		t.Fatalf("duplicate execution diagnostics = %v", duplicate.Diagnostics())
	}
	mismatch := NewComponent[specOtherID](
		Descriptor{DisplayName: "mismatched execution"},
		pluginSchema(1),
		WithSpec(testSpec(nil, nil)),
		WithProcessor("wrong", typ, "out", typ),
	)
	if !hasItem(mismatch.Diagnostics(), "plugin.execution-ports") {
		t.Fatalf("execution shape diagnostics = %v", mismatch.Diagnostics())
	}
}

func TestRouterExecutionBindingRequiresManyOutputAndTypedRouter(t *testing.T) {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	spec := Spec[pluginConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(_ CompileContext, _ pluginConfig, inputs flow.Descriptors[int]) (Compiled[flow.Shape, int], error) {
			input, ok := inputs.One("in")
			if !ok {
				return Compiled[flow.Shape, int]{Requirements: []Requirement[int]{Require("in", ConditionNeed[int]("fixture.input"))}}, nil
			}
			return Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
		},
		Open: func(_ OpenContext, value flow.Shape) (flow.Operator, error) {
			return specRouterOperator{specOperator{shape: value}}, nil
		},
	}
	component := NewComponent[specUnitID](
		Descriptor{DisplayName: "typed router"},
		pluginSchema(1),
		WithSpec(spec),
		WithRouter("in", typ, "out", typ),
	)
	if diagnostics := component.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("router diagnostics = %v", diagnostics)
	}
	resolved, err := component.Resolve(componentPatch(t, component, "level", 1))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	operator, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	_ = operator.Close()

	mismatch := NewComponent[specOtherID](
		Descriptor{DisplayName: "router shape mismatch"},
		pluginSchema(1),
		WithSpec(testSpec(nil, nil)),
		WithRouter("in", typ, "out", typ),
	)
	if !hasItem(mismatch.Diagnostics(), "plugin.execution-ports") {
		t.Fatalf("router mismatch diagnostics = %v", mismatch.Diagnostics())
	}
}

func TestComponentTraitSlotIsTypedAndRejectsDuplicateKeys(t *testing.T) {
	key := TraitKeyOf[specTraitID]()
	component := NewComponent[specUnitID](
		Descriptor{DisplayName: "trait"},
		pluginSchema(1),
		WithSpec(testSpec(nil, nil)),
		WithTrait(key, "fixture=one", PortShapeRequired, 1),
	)
	value, ok := TraitValueOf[int](component, key)
	if !ok || value != 1 || len(component.Traits()) != 1 || component.Traits()[0].Manifest != "fixture=one" || component.Traits()[0].ShapeRequirement != PortShapeRequired {
		t.Fatalf("trait = %d/%v %#v", value, ok, component.Traits())
	}
	descriptors := component.Traits()
	descriptors[0].Manifest = "mutated"
	if component.Traits()[0].Manifest != "fixture=one" {
		t.Fatal("component trait descriptors retained caller mutation")
	}

	duplicate := NewComponent[specOtherID](
		Descriptor{DisplayName: "duplicate trait"},
		pluginSchema(1),
		WithSpec(testSpec(nil, nil)),
		WithTrait(key, "fixture=one", PortShapeRequired, 1),
		WithTrait(key, "fixture=two", PortShapeRequired, 2),
	)
	if !hasItem(duplicate.Diagnostics(), "plugin.trait-duplicate") {
		t.Fatalf("duplicate trait diagnostics = %v", duplicate.Diagnostics())
	}
}

func TestTraitOnlyComponentIsVisibleButNotExecutable(t *testing.T) {
	key := TraitKeyOf[specTraitID]()
	component := NewComponent[specUnitID](
		Descriptor{DisplayName: "control plane"},
		pluginSchema(1),
		WithTrait(key, "fixture=control", PortShapeOptional, 1),
	)
	if items := component.Diagnostics(); len(items) != 0 {
		t.Fatalf("trait-only diagnostics = %v", items)
	}
	view := component.View()
	if view.HasSpec || view.Executable || !view.Ports.Empty() || len(view.Traits) != 1 || view.Traits[0].ShapeRequirement != PortShapeOptional {
		t.Fatalf("trait-only view = %#v", view)
	}
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors[int]()); !errors.Is(err, ErrComponentSpec) {
		t.Fatalf("trait-only Compile error = %v", err)
	}
	if _, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), Compilation{}); !errors.Is(err, ErrComponentSpec) {
		t.Fatalf("trait-only Open error = %v", err)
	}

	empty := NewComponent[specOtherID](Descriptor{DisplayName: "empty"}, pluginSchema(1))
	if !hasItem(empty.Diagnostics(), "plugin.spec") {
		t.Fatalf("empty component diagnostics = %v", empty.Diagnostics())
	}
}

func TestCompileContextSharesMarkerTraitStore(t *testing.T) {
	key := TraitKeyOf[specTraitID]()
	base := CompileContext{}
	prepared, err := CompileContextWithTrait(base, key, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := TraitValueOf[int](base, key); ok {
		t.Fatal("base CompileContext was mutated")
	}
	value, ok := TraitValueOf[int](prepared, key)
	if !ok || value != 7 {
		t.Fatalf("prepared trait = %d/%v", value, ok)
	}
	if _, ok := TraitValueOf[string](prepared, key); ok {
		t.Fatal("prepared trait accepted the wrong type")
	}
	duplicate, err := CompileContextWithTrait(prepared, key, 8)
	if err != ErrDuplicateTrait {
		t.Fatalf("duplicate error = %v", err)
	}
	value, ok = TraitValueOf[int](duplicate, key)
	if !ok || value != 7 {
		t.Fatalf("duplicate changed context = %d/%v", value, ok)
	}
}

func TestCompileContextExposesCancellationWithoutValues(t *testing.T) {
	type contextKey struct{}

	key := TraitKeyOf[specTraitID]()
	prepared, err := CompileContextWithTrait(CompileContext{}, key, 7)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "hidden"), deadline)
	contextual := CompileContextWithContext(prepared, parent)

	gotDeadline, ok := contextual.Context().Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("deadline = %v/%v, want %v", gotDeadline, ok, deadline)
	}
	if value := contextual.Context().Value(contextKey{}); value != nil {
		t.Fatalf("context value = %v, want nil", value)
	}
	value, ok := TraitValueOf[int](contextual, key)
	if !ok || value != 7 {
		t.Fatalf("prepared trait = %d/%v", value, ok)
	}

	cancel()
	select {
	case <-contextual.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("cancellation was not propagated")
	}
	if !errors.Is(contextual.Context().Err(), context.Canceled) {
		t.Fatalf("context error = %v", contextual.Context().Err())
	}
	if (CompileContext{}).Context().Value(contextKey{}) != nil {
		t.Fatal("zero CompileContext exposed a value")
	}
}

func TestSpecCapturesCanonicalImplementationContract(t *testing.T) {
	spec := testSpec(nil, nil)
	component := NewComponent[specUnitID](Descriptor{DisplayName: "spec"}, pluginSchema(1), WithSpec(spec))
	if got := component.Contract(); !reflect.DeepEqual(got, DefaultContract()) {
		t.Fatalf("default contract = %#v", got)
	}

	features := []string{"sse4.2", "avx2"}
	spec.Contract = Contract{
		Accuracy:       BoundedContract,
		Repeatability:  VariableContract,
		Artifact:       StableArtifactSupport,
		Implementation: PureGoImplementation | SIMDImplementation,
		Platform:       PlatformRequirement{OS: "linux", Arch: "amd64", Features: features},
	}
	component = NewComponent[specOtherID](Descriptor{DisplayName: "contract"}, pluginSchema(1), WithSpec(spec))
	features[0] = "changed"
	contract := component.Contract()
	if !contract.Valid() || len(contract.Platform.Features) != 2 || contract.Platform.Features[0] != "avx2" || contract.Platform.Features[1] != "sse4.2" {
		t.Fatalf("canonical contract = %#v", contract)
	}
	contract.Platform.Features[0] = "changed"
	if component.View().Contract.Platform.Features[0] != "avx2" {
		t.Fatal("component contract retained caller storage")
	}

	spec.Contract.Platform.Features = []string{"avx2", "avx2"}
	invalid := NewComponent[struct{ Invalid bool }](Descriptor{DisplayName: "invalid"}, pluginSchema(1), WithSpec(spec))
	found := false
	for _, item := range invalid.Diagnostics() {
		found = found || item.Code == "plugin.contract"
	}
	if !found {
		t.Fatalf("invalid contract diagnostics = %v", invalid.Diagnostics())
	}
}

func TestCompilationCannotOpenThroughAnotherSpec(t *testing.T) {
	first := NewComponent[specUnitID](Descriptor{DisplayName: "first"}, pluginSchema(1), WithSpec(testSpec(nil, nil)))
	second := NewComponent[specOtherID](Descriptor{DisplayName: "second"}, pluginSchema(1), WithSpec(testSpec(nil, nil)))
	resolved, _ := first.Resolve(config.NewPatch())
	compiled, err := Compile(first, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Open(NewOpenContext(context.Background(), OpenServices{}), compiled); err != ErrCompilationComponent {
		t.Fatalf("cross-component Open error = %v", err)
	}
}

func TestSuggestIsBoundedCanonicalAndDoesNotOpen(t *testing.T) {
	var opened atomic.Int32
	suggest := func(_ SuggestContext, _ int, _ Need[int]) []pluginConfig {
		return []pluginConfig{{Level: 2}, {Level: 3}}
	}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "suggest"}, pluginSchema(1), WithSpec(testSpec(&opened, suggest)))
	first, err := Suggest(component, SuggestContext{}, 1, ConditionNeed[int]("fixture.target"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Suggest(component, SuggestContext{}, 1, ConditionNeed[int]("fixture.target"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 || first[0].Fingerprint() != second[0].Fingerprint() || first[1].Fingerprint() != second[1].Fingerprint() {
		t.Fatalf("suggestions are not deterministic: %#v %#v", first, second)
	}
	if opened.Load() != 0 {
		t.Fatal("Suggest opened an operator")
	}

	duplicate := func(_ SuggestContext, _ int, _ Need[int]) []pluginConfig {
		return []pluginConfig{{Level: 2}, {Level: 2}}
	}
	component = NewComponent[specUnitID](Descriptor{DisplayName: "duplicate"}, pluginSchema(1), WithSpec(testSpec(nil, duplicate)))
	if _, err := Suggest(component, SuggestContext{}, 1, ConditionNeed[int]("fixture.target")); !hasDiagnostic(err, "plugin.suggest-duplicate") {
		t.Fatalf("duplicate Suggest error = %v", err)
	}

	overLimit := func(_ SuggestContext, _ int, _ Need[int]) []pluginConfig {
		return []pluginConfig{{Level: 1}, {Level: 2}, {Level: 3}, {Level: 4}}
	}
	component = NewComponent[specUnitID](Descriptor{DisplayName: "limit"}, pluginSchema(1), WithSpec(testSpec(nil, overLimit)))
	if _, err := Suggest(component, SuggestContext{}, 1, ConditionNeed[int]("fixture.target")); !hasDiagnostic(err, "plugin.suggest-limit") {
		t.Fatalf("over-limit Suggest error = %v", err)
	}
}

func TestComponentSpecRejectsMissingPhaseOrInvalidPorts(t *testing.T) {
	withoutSpec := NewComponent[specUnitID](Descriptor{DisplayName: "missing spec"}, pluginSchema(1))
	if !hasItem(withoutSpec.Diagnostics(), "plugin.spec") {
		t.Fatalf("missing Spec diagnostics = %v", withoutSpec.Diagnostics())
	}
	duplicate := NewComponent[specUnitID](Descriptor{DisplayName: "duplicate spec"}, pluginSchema(1), WithSpec(testSpec(nil, nil)), WithSpec(testSpec(nil, nil)))
	if !hasItem(duplicate.Diagnostics(), "plugin.spec") {
		t.Fatalf("duplicate Spec diagnostics = %v", duplicate.Diagnostics())
	}
	missing := NewComponent[specUnitID](Descriptor{DisplayName: "missing"}, pluginSchema(1), WithSpec(Spec[pluginConfig, struct{}, int]{}))
	if len(missing.Diagnostics()) < 3 {
		t.Fatalf("missing phase diagnostics = %v", missing.Diagnostics())
	}
	invalid := testSpec(nil, nil)
	invalid.Ports = flow.Shape{}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "invalid"}, pluginSchema(1), WithSpec(invalid))
	if !hasItem(component.Diagnostics(), "plugin.ports") {
		t.Fatalf("invalid Ports diagnostics = %v", component.Diagnostics())
	}
}

func TestComponentPortsAreStaticAndImmutable(t *testing.T) {
	spec := testSpec(nil, nil)
	component := NewComponent[specUnitID](Descriptor{DisplayName: "static ports"}, pluginSchema(1), WithSpec(spec))
	spec.Ports.Inputs[0] = flow.Port{}
	first := component.Ports()
	first.Inputs[0] = flow.Port{}
	second := component.Ports()
	if second.Inputs[0].ID() != "in" {
		t.Fatalf("Ports retained caller mutation: %#v", second)
	}
	if !component.View().Ports.Equal(second) {
		t.Fatal("component view Ports diverged from the component Ports")
	}
}

func hasDiagnostic(err error, code string) bool {
	return hasItem(diagnostic.ItemsOf(err), code)
}

func hasItem(items []diagnostic.Item, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func TestCompileVisibleResultIsRepeatable(t *testing.T) {
	component := NewComponent[specUnitID](Descriptor{DisplayName: "repeatable"}, pluginSchema(1), WithSpec(testSpec(nil, nil)))
	resolved, _ := component.Resolve(componentPatch(t, component, "level", 2))
	inputs := flow.NewDescriptors(flow.Describe("in", 5))
	first, err := Compile(component, CompileContext{}, resolved, inputs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(component, CompileContext{}, resolved, inputs)
	if err != nil {
		t.Fatal(err)
	}
	firstOutputs, _ := OutputsOf[int](first)
	secondOutputs, _ := OutputsOf[int](second)
	if !reflect.DeepEqual(firstOutputs.Bindings(), secondOutputs.Bindings()) || !reflect.DeepEqual(first.Effects(), second.Effects()) {
		t.Fatal("Compile visible result changed across identical calls")
	}
}

func TestCompileRejectsIncompleteContractResults(t *testing.T) {
	missing := testSpec(nil, nil)
	missing.Compile = func(CompileContext, pluginConfig, flow.Descriptors[int]) (Compiled[specPlan, int], error) {
		return Compiled[specPlan, int]{Outputs: flow.NewDescriptors[int]()}, nil
	}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "incomplete"}, pluginSchema(1), WithSpec(missing))
	resolved, _ := component.Resolve(config.NewPatch())
	if _, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors[int]()); !hasDiagnostic(err, "plugin.compile-missing-requirement") {
		t.Fatalf("missing input requirement error = %v", err)
	}
	if _, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1))); !hasDiagnostic(err, "plugin.compile-required-output") {
		t.Fatalf("missing required output error = %v", err)
	}

	invalidEffect := testSpec(nil, nil)
	invalidEffect.Compile = func(_ CompileContext, _ pluginConfig, inputs flow.Descriptors[int]) (Compiled[specPlan, int], error) {
		input, _ := inputs.One("in")
		return Compiled[specPlan, int]{
			Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			Effects: []Effect{{Kind: RepresentationEffect, Loss: NoLoss}},
		}, nil
	}
	component = NewComponent[specUnitID](Descriptor{DisplayName: "invalid effect"}, pluginSchema(1), WithSpec(invalidEffect))
	resolved, _ = component.Resolve(config.NewPatch())
	if _, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1))); !hasDiagnostic(err, "plugin.compile-effect") {
		t.Fatalf("invalid effect error = %v", err)
	}
}

func TestCompileRejectsDuplicateRequirementsForOnePort(t *testing.T) {
	spec := testSpec(nil, nil)
	spec.Compile = func(CompileContext, pluginConfig, flow.Descriptors[int]) (Compiled[specPlan, int], error) {
		need := ConditionNeed[int]("fixture.input")
		return Compiled[specPlan, int]{Requirements: []Requirement[int]{Require("in", need), Require("in", need)}}, nil
	}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "duplicate requirements"}, pluginSchema(1), WithSpec(spec))
	resolved, _ := component.Resolve(config.NewPatch())
	if _, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1))); !hasDiagnostic(err, "plugin.compile-duplicate-requirement") {
		t.Fatalf("duplicate requirement error = %v", err)
	}
}

func TestOpenRejectsOperatorShapeDifferentFromCompilation(t *testing.T) {
	var closed atomic.Int32
	spec := testSpec(nil, nil)
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	wrongShape := flow.NewShape(nil, []flow.Port{flow.Out("different", typ)})
	spec.Open = func(OpenContext, specPlan) (flow.Operator, error) {
		return trackedSpecOperator{shape: wrongShape, closed: &closed}, nil
	}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "wrong shape"}, pluginSchema(1), WithSpec(spec))
	resolved, _ := component.Resolve(config.NewPatch())
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled); !hasDiagnostic(err, "plugin.open-shape") {
		t.Fatalf("Open shape error = %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("incompatible operator close count = %d", closed.Load())
	}
}

func TestOpenEnforcesDeclaredFinalizerCapability(t *testing.T) {
	spec := testSpec(nil, nil)
	spec.Finalizes = true
	spec.Compile = func(_ CompileContext, value pluginConfig, inputs flow.Descriptors[int]) (Compiled[specPlan, int], error) {
		input, _ := inputs.One("in")
		return Compiled[specPlan, int]{
			Plan:         specPlan{shape: spec.Ports},
			Outputs:      flow.NewDescriptors(flow.Describe("out", input+value.Level)),
			Finalization: RequiresFinalization,
		}, nil
	}
	component := NewComponent[specUnitID](Descriptor{DisplayName: "finalizer"}, pluginSchema(1), WithSpec(spec))
	resolved, _ := component.Resolve(config.NewPatch())
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled); !hasDiagnostic(err, "plugin.open-finalizer") {
		t.Fatalf("missing finalizer error = %v", err)
	}

	spec.Open = func(_ OpenContext, plan specPlan) (flow.Operator, error) {
		return specFinalizerOperator{specOperator{shape: plan.shape}}, nil
	}
	component = NewComponent[specOtherID](Descriptor{DisplayName: "finalizer"}, pluginSchema(1), WithSpec(spec))
	resolved, _ = component.Resolve(config.NewPatch())
	compiled, err = Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	operator, err := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	_ = operator.Close()
}

type leakyConfigID struct{}
type leakyComponentID struct{}

type leakyConfig struct{ Token config.SecretValue[string] }

// A plugin panics with whatever value it chooses, and a plugin that panics
// while holding a credential chooses that. Every phase boundary recovers, so
// every one of them must report the failure without rendering the value.
func TestPhasePanicsNeverRenderTheRecoveredValue(t *testing.T) {
	const secret = "r01-plugin-panic-secret"
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	schemaValue := config.Struct[leakyConfigID](func() leakyConfig {
		return leakyConfig{Token: config.NewSecret(secret)}
	}).
		Version("1").
		AddField(config.Field("token", func(value *leakyConfig) *config.SecretValue[string] { return &value.Token }, config.SecretCodec(config.String()))).
		Build()
	leak := func(value leakyConfig) { panic(errors.New(value.Token.Reveal())) }
	compilePanics := true
	spec := Spec[leakyConfig, specPlan, int]{
		Ports: shape,
		Compile: func(_ CompileContext, value leakyConfig, _ flow.Descriptors[int]) (Compiled[specPlan, int], error) {
			if compilePanics {
				leak(value)
			}
			return Compiled[specPlan, int]{Plan: specPlan{shape: shape}, Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Suggest: func(SuggestContext, int, Need[int]) []leakyConfig {
			panic(errors.New(secret))
		},
		SuggestionLimit: 1,
		Open: func(OpenContext, specPlan) (flow.Operator, error) {
			panic(errors.New(secret))
		},
	}
	component := NewComponent[leakyComponentID](Descriptor{DisplayName: "leaky"}, schemaValue, WithSpec(spec))
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}

	_, compileErr := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	_, suggestErr := Suggest(component, SuggestContext{}, 1, ConditionNeed[int]("leak"))
	compilePanics = false
	compiled, err := Compile(component, CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", 1)))
	if err != nil {
		t.Fatal(err)
	}
	_, openErr := component.Open(NewOpenContext(context.Background(), OpenServices{}), compiled)
	for name, err := range map[string]error{
		"Compile": compileErr,
		"Suggest": suggestErr,
		"Open":    openErr,
	} {
		if err == nil {
			t.Errorf("%s reported no error for a panicking component", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s error exposed the panic value: %v", name, err)
		}
		for _, item := range diagnostic.ItemsOf(err) {
			for key, value := range item.Detail {
				if strings.Contains(value, secret) {
					t.Errorf("%s diagnostic detail %s exposed the panic value", name, key)
				}
			}
		}
	}
}
