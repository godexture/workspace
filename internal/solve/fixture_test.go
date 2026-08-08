package solve

import (
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type solvePluginID struct{}
type solveConfigID struct{}
type solveSourceID struct{}
type solveSinkID struct{}
type solveBridgeABID struct{}
type solveBridgeABSecondID struct{}
type solveBridgeBCID struct{}
type solveBridgeCDID struct{}
type solveBridgeAAID struct{}
type solveUnrelatedID struct{}
type solveMixerID struct{}
type solveSchemaAID struct{}
type solveSchemaBID struct{}
type solveSchemaCID struct{}
type solveSchemaDID struct{}
type solveUnit struct{}

var (
	solveSchemaA = schema.Define[solveSchemaAID, solveUnit](schema.Traits[solveUnit]{})
	solveSchemaB = schema.Define[solveSchemaBID, solveUnit](schema.Traits[solveUnit]{})
	solveSchemaC = schema.Define[solveSchemaCID, solveUnit](schema.Traits[solveUnit]{})
	solveSchemaD = schema.Define[solveSchemaDID, solveUnit](schema.Traits[solveUnit]{})
)

type solveConfig struct{ Mode int }

func solveConfigSchema() config.Schema[solveConfig] {
	return config.Struct[solveConfigID](func() solveConfig { return solveConfig{} }).
		Version("1").
		AddField(config.Field("mode", func(value *solveConfig) *int { return &value.Mode }, config.Int().Range(0, 8))).
		Build()
}

type solvePlan struct{ shape flow.Shape }
type solveOperator struct{ shape flow.Shape }

func (o solveOperator) Ports() flow.Shape { return o.shape.Clone() }
func (solveOperator) Close() error        { return nil }

type solveCompile func(solveConfig, flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor]

func solveComponent[Marker any](shape flow.Shape, compile solveCompile, suggest plugin.SuggestFunc[solveConfig, stream.Descriptor], suggestionLimit int, contract plugin.Contract, opened, compiles *atomic.Int32) plugin.Component {
	spec := plugin.Spec[solveConfig, solvePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[solveConfig](shape),
		Compile: func(_ plugin.CompileContext, value solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
			if compiles != nil {
				compiles.Add(1)
			}
			result := compile(value, inputs)
			result.Plan = solvePlan{shape: shape.Clone()}
			return result, nil
		},
		Suggest:         suggest,
		SuggestionLimit: suggestionLimit,
		Open: func(_ plugin.OpenContext, plan solvePlan) (flow.Operator, error) {
			if opened != nil {
				opened.Add(1)
			}
			return solveOperator{shape: plan.shape.Clone()}, nil
		},
		Contract: contract,
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "fixture"}, solveConfigSchema(), plugin.WithSpec(spec))
}

func solveSource(typ schema.Type[solveUnit], opened *atomic.Int32) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	return solveComponent[solveSourceID](shape, func(solveConfig, flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", solveDescriptor(typ, 44100)))}
	}, nil, 0, plugin.Contract{}, opened, nil)
}

func solveSink(typ schema.Type[solveUnit], condition bool, opened *atomic.Int32) plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	return solveComponent[solveSinkID](shape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok || condition && input.TimeBase().Denominator != 48000 {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.48000"))}}
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}
	}, nil, 0, plugin.Contract{}, opened, nil)
}

func solveBridge[Marker any](from, to schema.Type[solveUnit], effect plugin.Effect, transform func(stream.Descriptor, solveConfig) stream.Descriptor, suggest plugin.SuggestFunc[solveConfig, stream.Descriptor], limit int, contract plugin.Contract, opened, compiles *atomic.Int32) plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", from)}, []flow.Port{flow.Out("out", to)})
	return solveComponent[Marker](shape, func(value solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.input"))}}
		}
		output := transform(input, value)
		return plugin.Compiled[solvePlan, stream.Descriptor]{
			Outputs:   flow.NewDescriptors(flow.Describe("out", output)),
			Effects:   []plugin.Effect{effect},
			Estimate:  resource.Estimate{CPU: resource.Work(value.Mode + 1)},
			Resources: resource.Request{Memory: 1},
		}
	}, suggest, limit, contract, opened, compiles)
}

func schemaTransform(target schema.Type[solveUnit]) func(stream.Descriptor, solveConfig) stream.Descriptor {
	return func(input stream.Descriptor, _ solveConfig) stream.Descriptor {
		return stream.MustDescriptor(input.ID(), target.Identity(), input.TimeBase(), input.Properties()).WithMetadata(input.Metadata())
	}
}

func timeBaseTransform(input stream.Descriptor, _ solveConfig) stream.Descriptor {
	return stream.MustDescriptor(input.ID(), input.Schema(), timing.MustBase(1, 48000), input.Properties()).WithMetadata(input.Metadata())
}

func identityTransform(input stream.Descriptor, _ solveConfig) stream.Descriptor { return input }

func solveDescriptor(typ schema.Type[solveUnit], denominator int64) stream.Descriptor {
	return stream.MustDescriptor("stream", typ.Identity(), timing.MustBase(1, denominator), property.New())
}

func solveIndex(t testing.TB, components ...plugin.Component) catalog.Index {
	t.Helper()
	index, err := catalog.Build(plugin.NewSet(plugin.Define[solvePluginID](plugin.Descriptor{DisplayName: "solver", Version: "1"}, components...)))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func solveRequest(t testing.TB, source, sink plugin.Component, budget job.Budget) job.Job {
	t.Helper()
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, requested, job.WithBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func solvePlatform() plan.Platform {
	return plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}
}

func structural(detail string) plugin.Effect {
	return plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: detail}
}
