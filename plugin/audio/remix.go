package audio

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

// halfPower is the level a channel folded into two others is given so that the
// two together carry the power the one did.
const halfPower = -3.010299956639812

type remixConfig struct {
	Layout     sample.Layout
	Center     config.Optional[config.DecibelValue]
	Surround   config.Optional[config.DecibelValue]
	LowEnd     config.Optional[config.DecibelValue]
	Normalize  bool
	MaxSamples int
}

func remixSchema() config.Schema[remixConfig] {
	return config.Struct[remixConfigID](func() remixConfig {
		return remixConfig{
			Layout:     sample.Stereo(),
			Center:     config.Some[config.DecibelValue](halfPower),
			Surround:   config.Some[config.DecibelValue](halfPower),
			LowEnd:     config.None[config.DecibelValue](),
			Normalize:  true,
			MaxSamples: defaultFilterSamples,
		}
	}).
		Version("1").
		AddField(config.Field("layout", func(value *remixConfig) *sample.Layout { return &value.Layout },
			sample.LayoutCodec().Help("channels the stream is stated across afterwards"))).
		AddField(config.Field("center", func(value *remixConfig) *config.Optional[config.DecibelValue] { return &value.Center },
			config.OptionalCodec(config.Decibel().Range(-60, 12)).
				Help("level a centre channel is folded in at, or absent to drop it"))).
		AddField(config.Field("surround", func(value *remixConfig) *config.Optional[config.DecibelValue] { return &value.Surround },
			config.OptionalCodec(config.Decibel().Range(-60, 12)).
				Help("level a surround channel is folded in at, or absent to drop it"))).
		AddField(config.Field("lowEnd", func(value *remixConfig) *config.Optional[config.DecibelValue] { return &value.LowEnd },
			config.OptionalCodec(config.Decibel().Range(-60, 12)).
				Help("level the low-frequency channel is folded in at, or absent to drop it"))).
		AddField(config.Field("normalize", func(value *remixConfig) *bool { return &value.Normalize },
			config.Bool().Help("scale the result down when folding pushed it past full scale"))).
		AddField(budget(func(value *remixConfig) *int { return &value.MaxSamples })).
		Build()
}

func newRemix() plugin.Component {
	shape := filterShape()
	spec := plugin.Spec[remixConfig, producerPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration remixConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[producerPlan, stream.Descriptor], error) {
			return compileRemix(shape, configuration, inputs)
		},
		Open: func(ctx plugin.OpenContext, plan producerPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
			}
			return newProducerOperator(plan.shape, plan.kernel, plan.inputs, plan.outputs, plan.samples, ctx.Buffers()), nil
		},
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[remixID](plugin.Descriptor{DisplayName: "Remix"}, remixSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

// producerPlan carries what a reshaping filter settled at Compile. The kernel
// is built there rather than at Open because the matrix is a pure function of
// the configuration and the two layouts, and building it is what proves the
// pair can be folded at all.
type producerPlan struct {
	shape           flow.Shape
	kernel          producer
	inputs, outputs int
	samples         int
}

func compileRemix(shape flow.Shape, configuration remixConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[producerPlan, stream.Descriptor], error) {
	input, signal, incomplete, ready, err := processedInput[producerPlan](shape, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	if !configuration.Layout.Valid() {
		return plugin.Compiled[producerPlan, stream.Descriptor]{}, errors.New("remix needs the channels to state the stream across")
	}
	properties, err := reshaped(signal, configuration.Layout).Apply(input.Properties())
	if err != nil {
		return plugin.Compiled[producerPlan, stream.Descriptor]{}, err
	}
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), properties)
	if err != nil {
		return plugin.Compiled[producerPlan, stream.Descriptor]{}, err
	}
	inputChannels, outputChannels := signal.Layout.Count(), configuration.Layout.Count()
	return plugin.Compiled[producerPlan, stream.Descriptor]{
		Plan: producerPlan{
			shape:   shape.Clone(),
			kernel:  newRemixMatrix(signal.Layout, configuration),
			inputs:  inputChannels,
			outputs: outputChannels,
			samples: configuration.MaxSamples,
		},
		Outputs:   flow.NewDescriptors(flow.Describe("filtered", output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.ContentEffect, Loss: remixLoss(signal.Layout, configuration.Layout), Detail: "audio.remix"}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](configuration.MaxSamples, outputChannels))},
	}, nil
}

// remixLoss reports whether the fold can be undone. Spreading one channel over
// more of them keeps everything that arrived; folding several into fewer does
// not, because the sum cannot be taken apart again.
func remixLoss(from, to sample.Layout) plugin.Loss {
	if to.Count() >= from.Count() {
		return plugin.NoLoss
	}
	return plugin.Lossy
}

// remixMatrix is the whole of the fold: one gain per source channel per target
// channel, worked out once from the two layouts. What the loop then does is
// the same for every pair of layouts, so the rules about centres, surrounds
// and low frequency live here rather than in the arithmetic.
type remixMatrix struct {
	gains     [][]float32
	normalize bool
}

func newRemixMatrix(from sample.Layout, configuration remixConfig) *remixMatrix {
	to := configuration.Layout
	result := &remixMatrix{gains: make([][]float32, to.Count()), normalize: configuration.Normalize}
	for target := range result.gains {
		result.gains[target] = make([]float32, from.Count())
	}
	// Without positions on both sides there is nothing to match on, so the
	// channels line up in order and the extra ones on either side are dropped
	// or left silent.
	if !from.Positioned() || !to.Positioned() {
		for channel := range min(from.Count(), to.Count()) {
			result.gains[channel][channel] = 1
		}
		return result
	}
	for source := range from.Count() {
		position, _ := from.At(source)
		if target := to.Index(position); target >= 0 {
			result.gains[target][source] = 1
			continue
		}
		result.fold(to, position, source, configuration)
	}
	return result
}

// fold spreads a channel the target does not have over the ones it does. A
// mono target takes it whole; anything else takes it into the front pair,
// which is where a listener would otherwise have lost it.
func (m *remixMatrix) fold(to sample.Layout, position sample.Position, source int, configuration remixConfig) {
	level, present := foldLevel(position, configuration)
	if !present {
		return
	}
	gain := amplitude(float64(level))
	if to.Count() == 1 {
		m.gains[0][source] += gain
		return
	}
	left, right := to.Index(sample.FrontLeft), to.Index(sample.FrontRight)
	if left < 0 || right < 0 {
		return
	}
	m.gains[left][source] += gain
	m.gains[right][source] += gain
}

func foldLevel(position sample.Position, configuration remixConfig) (config.DecibelValue, bool) {
	switch position {
	case sample.FrontCenter, sample.FrontLeftOfCenter, sample.FrontRightOfCenter:
		return configuration.Center.Value, configuration.Center.Present
	case sample.LowFrequency:
		return configuration.LowEnd.Value, configuration.LowEnd.Present
	case sample.BackLeft, sample.BackRight, sample.BackCenter, sample.SideLeft, sample.SideRight:
		return configuration.Surround.Value, configuration.Surround.Present
	default:
		return 0, false
	}
}

func (m *remixMatrix) Produce(out, in [][]float32) {
	for target, row := range m.gains {
		clear(out[target])
		for source, gain := range row {
			if gain == 0 {
				continue
			}
			values := in[source]
			for index := range out[target] {
				out[target][index] += values[index] * gain
			}
		}
	}
	if !m.normalize {
		return
	}
	var loudest float32
	for _, samples := range out {
		if value := peakOf(samples); value > loudest {
			loudest = value
		}
	}
	if loudest <= 1 {
		return
	}
	for _, samples := range out {
		for index := range samples {
			samples[index] /= loudest
		}
	}
}
