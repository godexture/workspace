package plugin

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type specUnitID struct{}
type specOtherID struct{}
type specUnit struct{ Value int }
type specPlan struct {
	shape flow.Shape
	value int
}

type specOperator struct{ shape flow.Shape }

func (o specOperator) Ports() flow.Shape { return o.shape }
func (o specOperator) Close() error      { return nil }

type specProcessorOperator struct{ specOperator }

func (specProcessorOperator) Process(context.Context, flow.Input[specUnit], flow.Emitter[specUnit]) error {
	return nil
}

func (specProcessorOperator) Flush(context.Context, flow.Emitter[specUnit]) error { return nil }

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
		Shape: StaticShape[pluginConfig](shape),
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
	resolved, err := component.Resolve(config.NewPatch().Set("level", 2))
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
	if !hasItem(mismatch.Diagnostics(), "plugin.execution-shape") {
		t.Fatalf("execution shape diagnostics = %v", mismatch.Diagnostics())
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
	if len(first) != 2 || len(second) != 2 || first[0].Fingerprint != second[0].Fingerprint || first[1].Fingerprint != second[1].Fingerprint {
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

type mixerConfig struct{ Inputs int }
type mixerConfigID struct{}
type mixerComponentID struct{}

func TestDynamicShapeComesFromResolvedConfig(t *testing.T) {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	schemaValue := config.Struct[mixerConfigID](func() mixerConfig { return mixerConfig{Inputs: 2} }).
		Version("1").
		AddField(config.Field("inputs", func(value *mixerConfig) *int { return &value.Inputs }, config.Int().Range(1, 8))).
		Build()
	spec := Spec[mixerConfig, struct{}, int]{
		DynamicShape: true,
		Shape: func(_ ShapeContext, value mixerConfig) (flow.Shape, error) {
			inputs := make([]flow.Port, value.Inputs)
			for index := range inputs {
				inputs[index] = flow.In(fmt.Sprintf("in-%d", index), typ)
			}
			return flow.NewShape(inputs, []flow.Port{flow.Out("out", typ)}), nil
		},
		Compile: func(CompileContext, mixerConfig, flow.Descriptors[int]) (Compiled[struct{}, int], error) {
			return Compiled[struct{}, int]{Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(OpenContext, struct{}) (flow.Operator, error) {
			return specOperator{}, nil
		},
	}
	component := NewComponent[mixerComponentID](Descriptor{DisplayName: "mixer"}, schemaValue, WithSpec(spec))
	resolved, err := component.Resolve(config.NewPatch().Set("inputs", 4))
	if err != nil {
		t.Fatal(err)
	}
	shape, err := component.Shape(ShapeContext{}, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Inputs) != 4 || !component.View().DynamicShape {
		t.Fatalf("dynamic shape = %#v", shape)
	}
}

func TestComponentSpecRejectsMissingPhaseOrInvalidDefaultShape(t *testing.T) {
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
	invalid.Shape = StaticShape[pluginConfig](flow.Shape{})
	component := NewComponent[specUnitID](Descriptor{DisplayName: "invalid"}, pluginSchema(1), WithSpec(invalid))
	if !hasItem(component.Diagnostics(), "plugin.port-shape") {
		t.Fatalf("invalid default shape diagnostics = %v", component.Diagnostics())
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
	resolved, _ := component.Resolve(config.NewPatch().Set("level", 2))
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
		shape, _ := spec.Shape(ShapeContext{}, value)
		return Compiled[specPlan, int]{
			Plan:         specPlan{shape: shape},
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
