package solve

import (
	"context"
	"strconv"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	audioGateSourceID    struct{}
	audioGateFilterID    struct{}
	audioGateSinkID      struct{}
	audioGateToFloatID   struct{}
	audioGateToIntegerID struct{}
)

func TestCompatibleAudioFilterRegionUsesOnlyBoundaryConverters(t *testing.T) {
	for _, filters := range []int{1, 4, 16} {
		t.Run("N="+strconv.Itoa(filters), func(t *testing.T) {
			index := audioGateIndex(t)
			request := audioGateRequest(t, filters)
			compiled, err := Resolve(context.Background(), index, request, solvePlatform())
			if err != nil {
				t.Fatal(err)
			}

			converters := 0
			toFloat := 0
			toInteger := 0
			for _, node := range compiled.Plan().Nodes() {
				if node.Origin == plan.Automatic {
					converters++
					switch node.Component {
					case plugin.IdentityOf[audioGateToFloatID]().String():
						toFloat++
					case plugin.IdentityOf[audioGateToIntegerID]().String():
						toInteger++
					default:
						t.Fatalf("unexpected automatic component %s", node.Component)
					}
				}
				if node.Component == plugin.IdentityOf[audioGateFilterID]().String() {
					if len(node.Inputs) != 1 || len(node.Outputs) != 1 ||
						node.Inputs[0].Descriptor.Schema != sample.F32().Identity().String() ||
						node.Outputs[0].Descriptor.Schema != sample.F32().Identity().String() {
						t.Fatalf("filter selected schema = %#v -> %#v", node.Inputs, node.Outputs)
					}
				}
			}
			if converters != 2 || toFloat != 1 || toInteger != 1 {
				t.Fatalf("N=%d converters = %d (to F32=%d, to S16=%d), want two boundary converters", filters, converters, toFloat, toInteger)
			}
		})
	}
}

func audioGateIndex(t testing.TB) catalog.Index {
	t.Helper()
	s16 := sample.S16()
	f32 := sample.F32()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", s16)})
	filterShape := flow.NewShape([]flow.Port{flow.In("in", f32)}, []flow.Port{flow.Out("out", f32)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", s16)}, nil)
	toFloatShape := flow.NewShape([]flow.Port{flow.In("in", s16)}, []flow.Port{flow.Out("out", f32)})
	toIntegerShape := flow.NewShape([]flow.Port{flow.In("in", f32)}, []flow.Port{flow.Out("out", s16)})
	sourceDescriptor := audioGateDescriptor(t, sample.S16Planar)

	source := solveComponent[audioGateSourceID](sourceShape, func(solveConfig, flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", sourceDescriptor))}
	}, nil, 0, plugin.Contract{}, nil, nil)
	filter := solveComponent[audioGateFilterID](filterShape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("audio.filter-input"))}}
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{
			Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			Effects: []plugin.Effect{{Kind: plugin.ContentEffect, Loss: plugin.NoLoss, Detail: "audio.synthetic-filter"}},
		}
	}, nil, 0, plugin.Contract{}, nil, nil)
	sink := solveComponent[audioGateSinkID](sinkShape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("audio.sink-input"))}}
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}
	}, nil, 0, plugin.Contract{}, nil, nil)
	toFloat := audioGateConverter[audioGateToFloatID](toFloatShape, sample.F32Planar)
	toInteger := audioGateConverter[audioGateToIntegerID](toIntegerShape, sample.S16Planar)
	return solveIndex(t, source, filter, sink, toFloat, toInteger)
}

func audioGateConverter[Marker any](shape flow.Shape, target sample.Format) plugin.Component {
	return solveComponent[Marker](shape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("audio.converter-input"))}}
		}
		output := audioGateDescriptorFrom(timing.MustBase(1, 48_000), input, target)
		return plugin.Compiled[solvePlan, stream.Descriptor]{
			Outputs: flow.NewDescriptors(flow.Describe("out", output)),
			Effects: []plugin.Effect{{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "audio.sample-conversion"}},
		}
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func audioGateRequest(t testing.TB, filters int) job.Job {
	t.Helper()
	nodes := []job.Node{job.NewNode("source", plugin.IdentityOf[audioGateSourceID](), config.NewPatch())}
	edges := make([]job.Edge, 0, filters+1)
	previous := job.At("source", "out")
	for index := 0; index < filters; index++ {
		id := job.NodeID("filter-" + strconv.Itoa(index))
		nodes = append(nodes, job.NewNode(id, plugin.IdentityOf[audioGateFilterID](), config.NewPatch()))
		edges = append(edges, job.Connect(previous, job.At(id, "in")))
		previous = job.At(id, "out")
	}
	nodes = append(nodes, job.NewNode("sink", plugin.IdentityOf[audioGateSinkID](), config.NewPatch()))
	edges = append(edges, job.Connect(previous, job.At("sink", "in")))
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func audioGateDescriptor(t testing.TB, format sample.Format) stream.Descriptor {
	t.Helper()
	description := sample.Description{Format: format, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.NoEndian}
	if format == sample.F32Planar {
		description.ValidBits = 32
	}
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	schemaDescriptor := sample.S16().Descriptor()
	if format == sample.F32Planar {
		schemaDescriptor = sample.F32().Descriptor()
	}
	return stream.MustDescriptor("audio", schemaDescriptor, timing.MustBase(1, 48_000), properties)
}

func audioGateDescriptorFrom(base timing.Base, input stream.Descriptor, format sample.Format) stream.Descriptor {
	description := sample.Description{Format: format, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.NoEndian}
	schemaDescriptor := sample.S16().Descriptor()
	if format == sample.F32Planar {
		description.ValidBits = 32
		schemaDescriptor = sample.F32().Descriptor()
	}
	properties, _ := description.Apply(input.Properties())
	return stream.MustDescriptor(input.ID(), schemaDescriptor, base, properties).WithMetadata(input.Metadata())
}
