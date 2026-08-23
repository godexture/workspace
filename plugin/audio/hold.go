package audio

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

// holdingPlan is what every stage that keeps part of the stream settled at
// Compile: the shape of the stream, how long it lasts if anything said, and
// how much it may hold while it works out what to emit.
type holdingPlan struct {
	shape    flow.Shape
	signal   sample.Signal
	channels int
	samples  int
	bytes    config.ByteValue
	// length is how long the stream lasts, and stated says whether anything
	// claimed it. A stage that needs the end and was told nothing has to hold
	// the stream to find it.
	length int64
	stated bool
}

// holding is the state those stages share: somewhere to put what they keep,
// and somewhere to build the frames they give back.
type holding struct {
	shape flow.Shape
	store *store
	lease *frameLease
	out   flow.Item[mediaaudio.Frame[float32]]
}

func (h *holding) Ports() flow.Shape { return h.shape.Clone() }
func (h *holding) Close() error      { return nil }

func openHolding(ctx plugin.OpenContext, plan holdingPlan) (*holding, error) {
	if ctx.Buffers() == nil {
		return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
	}
	held, err := newStore(ctx.Temporary(), plan.channels, plan.samples)
	if err != nil {
		return nil, err
	}
	return &holding{shape: plan.shape.Clone(), store: held, lease: newFrameLease(ctx.Buffers(), plan.channels)}, nil
}

// compileHolding settles what those stages have in common: the samples they
// read, the stream they hand back unchanged, and the claim on the store they
// will grow. settle turns that into the plan one of them actually runs on.
func compileHolding[P any](shape flow.Shape, detail string, samples int, bytes config.ByteValue, inputs flow.Descriptors[stream.Descriptor], settle func(holdingPlan) (P, error)) (plugin.Compiled[P, stream.Descriptor], error) {
	input, signal, incomplete, ready, err := processedInput[P](shape, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), input.Properties())
	if err != nil {
		return plugin.Compiled[P, stream.Descriptor]{}, err
	}
	length, stated := stream.DurationOf(input.Properties())
	plan, err := settle(holdingPlan{
		shape:    shape.Clone(),
		signal:   signal,
		channels: signal.Layout.Count(),
		samples:  samples,
		bytes:    bytes,
		length:   length.Value().Int64(),
		stated:   stated,
	})
	if err != nil {
		return plugin.Compiled[P, stream.Descriptor]{}, err
	}
	return plugin.Compiled[P, stream.Descriptor]{
		Plan:      plan,
		Outputs:   flow.NewDescriptors(flow.Describe(shape.Outputs[0].ID(), output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.ContentEffect, Loss: plugin.Lossy, Detail: detail}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](samples, signal.Layout.Count()))},
		Temporary: resource.Bytes(bytes),
	}, nil
}

// holdingBytes declares the ceiling on what a stage keeps. It is a ceiling
// rather than a reservation because nothing knows how long the stream is until
// it has passed, which is the whole reason these stages exist.
func holdingBytes[C any](accessor func(*C) *config.ByteValue, help string) config.FieldSpec[C] {
	return config.Field("maxBytes", accessor, config.Bytes().Range(1<<10, 1<<40).Help(help))
}
