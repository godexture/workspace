// Package audio implements the two things that happen to decoded audio: the
// converters a planner puts between stages that agree on a stream but not on
// how its samples are stored, and the processors that change the signal
// itself. Both work on canonical planar frames, which is what keeps the
// conversion at the edge of a chain of processors rather than inside each one.
package audio

import (
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type (
	pluginID struct{}

	s16ToS32ID struct{}
	s16ToF32ID struct{}
	s16ToF64ID struct{}
	s32ToS16ID struct{}
	s32ToF32ID struct{}
	s32ToF64ID struct{}
	f32ToS16ID struct{}
	f32ToS32ID struct{}
	f32ToF64ID struct{}
	f64ToS16ID struct{}
	f64ToS32ID struct{}
	f64ToF32ID struct{}

	gainID             struct{}
	gainConfigID       struct{}
	dcOffsetID         struct{}
	dcOffsetConfigID   struct{}
	compressorID       struct{}
	compressorConfigID struct{}
	gateID             struct{}
	gateConfigID       struct{}
	equalizerID        struct{}
	equalizerConfigID  struct{}
	equalizerBandID    struct{}
	delayID            struct{}
	delayConfigID      struct{}
	reverbID           struct{}
	reverbConfigID     struct{}
	remixID            struct{}
	remixConfigID      struct{}
	resampleID         struct{}
	resampleConfigID   struct{}
	retimeID           struct{}
	retimeConfigID     struct{}
	normalizeID        struct{}
	normalizeConfigID  struct{}
	fadeID             struct{}
	fadeConfigID       struct{}
	trimID             struct{}
	trimConfigID       struct{}
	convolverID        struct{}
	convolverConfigID  struct{}
	mixerID            struct{}
	mixerConfigID      struct{}
)

type conversion struct {
	from, to  sample.Coding
	identity  plugin.Identity
	component plugin.Component
}

// conversions covers every ordered pair of canonical decoded codings. A port
// schema is static, so a converter exists per pair rather than one component
// that reshapes itself.
var conversions = []conversion{
	newConverter[s16ToS32ID, int16, int32](),
	newConverter[s16ToF32ID, int16, float32](),
	newConverter[s16ToF64ID, int16, float64](),
	newConverter[s32ToS16ID, int32, int16](),
	newConverter[s32ToF32ID, int32, float32](),
	newConverter[s32ToF64ID, int32, float64](),
	newConverter[f32ToS16ID, float32, int16](),
	newConverter[f32ToS32ID, float32, int32](),
	newConverter[f32ToF64ID, float32, float64](),
	newConverter[f64ToS16ID, float64, int16](),
	newConverter[f64ToS32ID, float64, int32](),
	newConverter[f64ToF32ID, float64, float32](),
}

func newConverter[Marker any, From, To mediaaudio.Sample]() conversion {
	return conversion{
		from:      sample.CodingOf[From](),
		to:        sample.CodingOf[To](),
		identity:  plugin.IdentityOf[Marker](),
		component: newComponent[Marker, From, To](),
	}
}

// ConverterIdentity returns the component that restores a stream stored as one
// canonical coding into another. Codings that are equal, or that no canonical
// frame stores, have no converter.
func ConverterIdentity(from, to sample.Coding) plugin.Identity {
	for _, value := range conversions {
		if value.from == from && value.to == to {
			return value.identity
		}
	}
	return plugin.Identity{}
}

// Processor names one signal processor. The name is the stable spelling a
// surface uses to ask for it, so it is part of this family's contract rather
// than a display string.
type Processor string

const (
	Gain       Processor = "gain"
	DCOffset   Processor = "dc-offset"
	Compressor Processor = "compressor"
	Gate       Processor = "gate"
	Equalizer  Processor = "equalizer"
	Delay      Processor = "delay"
	Reverb     Processor = "reverb"
	Remix      Processor = "remix"
	Resample   Processor = "resample"
	Retime     Processor = "retime"
	Normalize  Processor = "normalize"
	Fade       Processor = "fade"
	Trim       Processor = "trim"
	Mixer      Processor = "mixer"
	Convolver  Processor = "convolver"
)

type entry struct {
	name      Processor
	component plugin.Component
}

// processors is ordered so the family always presents its components in the
// same sequence, which a map could not promise.
var processors = []entry{
	{name: Gain, component: newGain()},
	{name: DCOffset, component: newDCOffset()},
	{name: Compressor, component: newCompressor()},
	{name: Gate, component: newGate()},
	{name: Equalizer, component: newEqualizer()},
	{name: Delay, component: newDelay()},
	{name: Reverb, component: newReverb()},
	{name: Remix, component: newRemix()},
	{name: Resample, component: newResample()},
	{name: Retime, component: newRetime()},
	{name: Normalize, component: newNormalize()},
	{name: Fade, component: newFade()},
	{name: Trim, component: newTrim()},
	{name: Mixer, component: newMixer()},
	{name: Convolver, component: newConvolverComponent()},
}

// ProcessorIdentity returns the component implementing one processor. An
// unknown name yields the zero identity.
func ProcessorIdentity(name Processor) plugin.Identity {
	for _, value := range processors {
		if value.name == name {
			return value.component.Identity()
		}
	}
	return plugin.Identity{}
}

// Processors returns every processor name this family provides, in order.
func Processors() []Processor {
	result := make([]Processor, 0, len(processors))
	for _, value := range processors {
		result = append(result, value.name)
	}
	return result
}

// Plugin returns the pure-Go audio data plane family.
func Plugin() plugin.Definition {
	components := make([]plugin.Component, 0, len(conversions)+len(processors))
	for _, value := range conversions {
		components = append(components, value.component)
	}
	for _, value := range processors {
		components = append(components, value.component)
	}
	definition := plugin.Define[pluginID](plugin.Descriptor{
		DisplayName: "Audio",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}, components...)
	return definition.WithDeclarations(append(sample.Declarations(), stream.Declarations()...)...)
}

// Set returns the self-contained composition.
func Set() plugin.Set { return plugin.NewSet(Plugin()) }
