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
						node.Inputs[0].Descriptor.Schema != sample.Frames[float32]().Identity().String() ||
						node.Outputs[0].Descriptor.Schema != sample.Frames[float32]().Identity().String() {
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
	s16 := sample.Frames[int16]()
	f32 := sample.Frames[float32]()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", s16)})
	filterShape := flow.NewShape([]flow.Port{flow.In("in", f32)}, []flow.Port{flow.Out("out", f32)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", s16)}, nil)
	toFloatShape := flow.NewShape([]flow.Port{flow.In("in", s16)}, []flow.Port{flow.Out("out", f32)})
	toIntegerShape := flow.NewShape([]flow.Port{flow.In("in", f32)}, []flow.Port{flow.Out("out", s16)})
	sourceDescriptor := audioGateDescriptor(t, sample.S16)

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
	toFloat := audioGateConverter[audioGateToFloatID](toFloatShape, sample.F32)
	toInteger := audioGateConverter[audioGateToIntegerID](toIntegerShape, sample.S16)
	return solveIndex(t, source, filter, sink, toFloat, toInteger)
}

func audioGateConverter[Marker any](shape flow.Shape, target sample.Coding) plugin.Component {
	return solveComponent[Marker](shape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("audio.converter-input"))}}
		}
		properties, _ := audioGateDescription(target).Apply(input.Properties())
		schemaDescriptor, _ := sample.Schema(target)
		output := stream.MustDescriptor(input.ID(), schemaDescriptor, timing.MustBase(1, 48_000), properties).WithMetadata(input.Metadata())
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

func audioGateDescription(coding sample.Coding) sample.Description {
	return sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: coding.Bits()},
		Coding:  coding,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
}

func audioGateDescriptor(t testing.TB, coding sample.Coding) stream.Descriptor {
	t.Helper()
	properties, err := audioGateDescription(coding).Properties()
	if err != nil {
		t.Fatal(err)
	}
	schemaDescriptor, ok := sample.Schema(coding)
	if !ok {
		t.Fatalf("%s has no canonical frame schema", coding)
	}
	return stream.MustDescriptor("audio", schemaDescriptor, timing.MustBase(1, 48_000), properties)
}
