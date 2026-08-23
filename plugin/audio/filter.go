package audio

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

// Filter is one processor's arithmetic over the stream it was compiled for.
// It reads and writes that stream's planar float32 samples in place and is
// told nothing about how they are stored, because the planner puts the one
// conversion a region needs at the edge of the region rather than inside every
// stage. planes holds one slice per channel, all of the same length.
type filter interface {
	Apply(planes [][]float32)
}

// filterPlan is what Compile settled: the configuration the filter is built
// from, the stream it was built for, and the largest frame it will copy.
type filterPlan[C any] struct {
	shape    flow.Shape
	config   C
	signal   sample.Signal
	samples  int
	detail   string
	channels int
}

// filterShape is the same for every filter, which is the point: a stage that
// only changes sample values neither renames the stream nor reshapes it.
func filterShape() flow.Shape {
	frames := sample.Frames[float32]()
	return flow.NewShape(
		[]flow.Port{flow.In("frames", frames)},
		[]flow.Port{flow.Out("filtered", frames)},
	)
}

// budget declares the one field every filter shares. A filter edits its input
// in place while it owns it alone, and copies only the frames a fan-out left
// shared, so this bounds the copy rather than the ordinary path.
func budget[C any](accessor func(*C) *int) config.FieldSpec[C] {
	return config.Field("maxSamples", accessor, config.Int().Range(1, 1<<20).
		Help("largest frame this filter reserves copy-on-write planes for"))
}

const defaultFilterSamples = 8192

// filterSpec is what one filter contributes: how it is named, how it is
// configured, and how a configuration plus the stream it will run on becomes
// arithmetic. check is what a filter says about a configuration it can only
// judge once it knows the stream, so that judgement lands in planning rather
// than halfway through opening a graph.
type filterSpec[C any] struct {
	name    string
	detail  string
	schema  config.Schema[C]
	samples func(*C) *int
	check   func(C, sample.Signal) error
	build   func(C, sample.Signal) (filter, error)
}

// newFilter builds the component for one in-place filter. Everything that is
// the same for all of them lives here -- the ports, the samples they insist
// on, the descriptor the stream keeps, the exclusive-or-copy edit -- so a
// filter's own file holds its configuration and its arithmetic and nothing
// else.
func newFilter[Marker, C any](spec filterSpec[C]) plugin.Component {
	shape := filterShape()
	component := plugin.Spec[C, filterPlan[C], stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration C, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[filterPlan[C], stream.Descriptor], error) {
			return compileFilter(shape, spec, configuration, inputs)
		},
		Open: func(ctx plugin.OpenContext, plan filterPlan[C]) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
			}
			kernel, err := spec.build(plan.config, plan.signal)
			if err != nil {
				return nil, err
			}
			return newFilterOperator(plan, kernel, ctx.Buffers()), nil
		},
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: spec.name}, spec.schema,
		plugin.WithSpec(component),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

// compileFilter settles the one thing a filter asks of its input: samples it
// can read as float32 planes. Anything else is stated as the descriptor it
// wanted, so the planner reaches for a converter instead of the filter
// refusing a stream it could have been given.
func compileFilter[C any](shape flow.Shape, spec filterSpec[C], configuration C, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[filterPlan[C], stream.Descriptor], error) {
	input, signal, incomplete, ready, err := processedInput[filterPlan[C]](shape, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	if spec.check != nil {
		if err := spec.check(configuration, signal); err != nil {
			return plugin.Compiled[filterPlan[C], stream.Descriptor]{}, err
		}
	}
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), input.Properties())
	if err != nil {
		return plugin.Compiled[filterPlan[C], stream.Descriptor]{}, err
	}
	channels := signal.Layout.Count()
	return plugin.Compiled[filterPlan[C], stream.Descriptor]{
		Plan: filterPlan[C]{
			shape:    shape.Clone(),
			config:   configuration,
			signal:   signal,
			samples:  *spec.samples(&configuration),
			detail:   spec.detail,
			channels: channels,
		},
		Outputs:   flow.NewDescriptors(flow.Describe("filtered", output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.ContentEffect, Loss: plugin.Lossy, Detail: spec.detail}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](*spec.samples(&configuration), channels))},
	}, nil
}

// processedInput is the check every processor makes before anything else: it
// reads planar float32, or it states the descriptor it wanted so the planner
// bridges the stream rather than the processor refusing it. ready is false
// when the returned Compiled is the answer.
func processedInput[P any](shape flow.Shape, inputs flow.Descriptors[stream.Descriptor]) (stream.Descriptor, sample.Signal, plugin.Compiled[P, stream.Descriptor], bool, error) {
	input, ok := inputs.One(shape.Inputs[0].ID())
	if !ok {
		return stream.Descriptor{}, sample.Signal{}, plugin.Compiled[P, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{
				plugin.Require(shape.Inputs[0].ID(), plugin.ConditionNeed[stream.Descriptor]("audio.filter-input")),
			},
		}, false, nil
	}
	signal, err := sample.SignalOf(input.Properties())
	if err != nil {
		return stream.Descriptor{}, sample.Signal{}, plugin.Compiled[P, stream.Descriptor]{}, false, err
	}
	description, err := sample.FromProperties(input.Properties())
	if err == nil && description == processed(signal) && input.TimeBase() == timing.MustBase(1, int64(signal.Rate)) {
		return input, signal, plugin.Compiled[P, stream.Descriptor]{}, true, nil
	}
	desired, err := describeProcessed(input, shape.Inputs[0].Schema(), signal)
	if err != nil {
		return stream.Descriptor{}, sample.Signal{}, plugin.Compiled[P, stream.Descriptor]{}, false, err
	}
	return stream.Descriptor{}, sample.Signal{}, plugin.Compiled[P, stream.Descriptor]{
		Requirements: []plugin.Requirement[stream.Descriptor]{
			plugin.Require(shape.Inputs[0].ID(), plugin.DescriptorNeed("audio.filter-samples", desired)),
		},
	}, false, nil
}

// processed is the one representation every filter reads. Float samples carry
// the whole of their coding, so a filter never has to know how many bits the
// signal arrived with.
func processed(signal sample.Signal) sample.Description {
	signal.ValidBits = sample.F32.Bits()
	return sample.Description{Signal: signal, Coding: sample.F32, Packing: sample.Planar, Endian: sample.NoEndian}
}

func describeProcessed(input stream.Descriptor, schemaDescriptor schema.Descriptor, signal sample.Signal) (stream.Descriptor, error) {
	properties, err := processed(signal).Apply(input.Properties())
	if err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), schemaDescriptor, timing.MustBase(1, int64(signal.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}

var errFilterPlanes = errors.New("filter frame plane count does not match its channel layout")
