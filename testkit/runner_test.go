package testkit

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type runnerPluginID struct{}
type runnerComponentID struct{}
type runnerSchemaID struct{}
type runnerConfigID struct{}
type runnerFormatID struct{}
type runnerFormatComponentID struct{}
type runnerFormatPluginID struct{}
type runnerFormatConfigID struct{}

type runnerConfig struct{ Factor int }
type runnerFormatConfig struct{}
type runnerPlan struct {
	shape  flow.Shape
	factor int
}

var runnerType = schema.Define[runnerSchemaID](schema.Traits[int]{})

var (
	errRunnerPlan = errors.New("runner planning failure")
	errRunnerRun  = errors.New("runner execution failure")
)

func TestCommonRunnerExecutesSuccessFailureAndCoverage(t *testing.T) {
	definition := runnerDefinition()
	descriptor := stream.MustDescriptor("fixture", runnerType.Identity(), timing.MustBase(1, 1), property.New())
	coverage := NewCoverage()
	subject := Track(SubjectOf(definition, plugin.IdentityOf[runnerComponentID](), "in", runnerType, "out", runnerType), coverage)

	Component(t, subject,
		Case[int, int]{
			Name:   "success",
			Config: config.NewPatch().Set("factor", 2),
			Input:  Values(descriptor, runnerType, 3, 5),
			Want:   EqualValues(6, 10),
		},
		Case[int, int]{
			Name:   "failure-cleanup",
			Config: config.NewPatch().Set("factor", 2),
			Input:  Values(descriptor, runnerType, -1),
			Want:   Fails[int]("runner.negative"),
		},
		Case[int, int]{
			Name:   "execution-error",
			Config: config.NewPatch().Set("factor", 2),
			Input:  Values(descriptor, runnerType, -2),
			Want:   WantRunError[int](errRunnerRun),
		},
	)
	coverage.VerifyExecutable(t, plugin.NewSet(definition))
}

func TestFormatUsesAccessBoundaryInspection(t *testing.T) {
	var inspections atomic.Int32
	var probes atomic.Int32
	definition := runnerFormatDefinition(&inspections, &probes)
	subject := SubjectOf(definition, plugin.IdentityOf[runnerFormatComponentID](), "bytes", access.Bytes(), "out", access.Bytes())
	Format(t, subject,
		Case[buffer.Handle, buffer.Handle]{
			Name:  "inspected",
			Input: ByteInput([]byte{0x42}),
			Want:  WantBytes([]byte{0x42}),
		},
		Case[buffer.Handle, buffer.Handle]{
			Name:  "inspection-error",
			Input: ByteInput([]byte{0x43}),
			Want:  WantPlanError[buffer.Handle](errRunnerPlan),
		},
	)
	if got := inspections.Load(); got != 6 {
		t.Fatalf("Inspect calls = %d, want four successful and two rejected planning scenarios", got)
	}
	if got := probes.Load(); got != 4 {
		t.Fatalf("Probe calls = %d, want one request and one terminal result per case", got)
	}
}

func runnerDefinition() plugin.Definition {
	configuration := config.Struct[runnerConfigID](func() runnerConfig { return runnerConfig{Factor: 1} }).
		Version("1").
		AddField(config.Field("factor", func(value *runnerConfig) *int { return &value.Factor }, config.Int().Range(1, 8))).
		Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", runnerType)}, []flow.Port{flow.Out("out", runnerType)})
	spec := plugin.Spec[runnerConfig, runnerPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[runnerConfig](shape),
		Compile: func(_ plugin.CompileContext, value runnerConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[runnerPlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[runnerPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("runner.input")),
				}}, nil
			}
			return plugin.Compiled[runnerPlan, stream.Descriptor]{
				Plan:    runnerPlan{shape: shape.Clone(), factor: value.Factor},
				Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan runnerPlan) (flow.Operator, error) {
			return runnerOperator(plan), nil
		},
	}
	component := plugin.NewComponent[runnerComponentID](plugin.Descriptor{DisplayName: "runner fixture"}, configuration,
		plugin.WithSpec(spec), plugin.WithProcessor("in", runnerType, "out", runnerType))
	return plugin.Define[runnerPluginID](plugin.Descriptor{DisplayName: "runner fixture", Version: "1"}, component)
}

func runnerFormatDefinition(inspections, probes *atomic.Int32) plugin.Definition {
	configuration := config.Struct[runnerFormatConfigID](func() runnerFormatConfig { return runnerFormatConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", access.Bytes())})
	formatValue, _ := mediaformat.Define[runnerFormatID](nil)
	probeRange, _ := access.NewRangeRequest(0, 1)
	probe := func(ctx mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
		probes.Add(1)
		if len(ctx.Views()) == 0 {
			return mediaformat.Need(probeRange), nil
		}
		evidence, _ := mediaformat.NewEvidence("runner signature")
		return mediaformat.Match(evidence), nil
	}
	inspect := func(ctx mediaformat.InspectContext) (mediaformat.Inspection, error) {
		inspections.Add(1)
		reader, ok := access.RandomOf(ctx.Opening())
		if !ok {
			return mediaformat.Inspection{}, errors.New("random reader is absent")
		}
		value := []byte{0}
		if _, err := reader.ReadAt(ctx.Context(), value, 0); err != nil && !errors.Is(err, io.EOF) {
			return mediaformat.Inspection{}, err
		}
		if value[0] == 0x43 {
			return mediaformat.Inspection{}, errRunnerPlan
		}
		return mediaformat.NewInspection(formatValue, value[0]), nil
	}
	spec := plugin.Spec[runnerFormatConfig, runnerPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[runnerFormatConfig](shape),
		Compile: func(ctx plugin.CompileContext, _ runnerFormatConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[runnerPlan, stream.Descriptor], error) {
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[runnerPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("runner.format.input")),
				}}, nil
			}
			value, ok := mediaformat.InspectionOf[byte](ctx, formatValue)
			if !ok || value != 0x42 {
				return plugin.Compiled[runnerPlan, stream.Descriptor]{}, errors.New("prepared inspection is absent")
			}
			return plugin.Compiled[runnerPlan, stream.Descriptor]{
				Plan:    runnerPlan{shape: shape.Clone(), factor: 1},
				Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan runnerPlan) (flow.Operator, error) {
			return runnerByteOperator{shape: plan.shape}, nil
		},
	}
	component := plugin.NewComponent[runnerFormatComponentID](plugin.Descriptor{DisplayName: "runner format"}, configuration,
		plugin.WithSpec(spec),
		plugin.WithProcessor("bytes", access.Bytes(), "out", access.Bytes()),
		mediaformat.Read(formatValue, access.NewRequirements(access.AnyOf(access.RandomRead)), mediaformat.WithProbe(probe), mediaformat.WithInspect(inspect)),
	)
	return plugin.Define[runnerFormatPluginID](plugin.Descriptor{DisplayName: "runner format", Version: "1"}, component)
}

type runnerOperator runnerPlan

func (o runnerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (runnerOperator) Close() error        { return nil }
func (o runnerOperator) Process(ctx context.Context, input flow.Input[int], output flow.Emitter[int]) error {
	if input.Value() == -2 {
		return errRunnerRun
	}
	if input.Value() < 0 {
		return diagnostic.NewError(diagnostic.NewItem("runner.negative", diagnostic.ErrorSeverity, diagnostic.Path{}, "negative fixture", nil))
	}
	value := flow.NewInput(input.Value()*o.factor, runnerType)
	if err := output.Emit(ctx, value); err != nil {
		return err
	}
	input.Drop()
	return nil
}
func (runnerOperator) Flush(context.Context, flow.Emitter[int]) error { return nil }

type runnerByteOperator struct{ shape flow.Shape }

func (o runnerByteOperator) Ports() flow.Shape { return o.shape.Clone() }
func (runnerByteOperator) Close() error        { return nil }
func (runnerByteOperator) Process(ctx context.Context, input flow.Input[buffer.Handle], output flow.Emitter[buffer.Handle]) error {
	owned := input.Take()
	if err := output.Emit(ctx, flow.NewInput(owned.Value(), access.Bytes())); err != nil {
		owned.Release()
		return err
	}
	return nil
}
func (runnerByteOperator) Flush(context.Context, flow.Emitter[buffer.Handle]) error { return nil }
