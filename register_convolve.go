package filter

import (
	"fmt"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/convolve"
	"github.com/godexture/sdk/engine"
)

func init() {
	registerConvolve()
}

func registerConvolve() {
	registerConvolveManifest()
}

func registerConvolveManifest() {
	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{
		BaseManifest: registry.BaseManifest{Name: "convolve", Description: "Apply FFT convolution against an impulse response", ConfigurationFactory: registry.NewConfigurationFactory(NewConvolutionConfig)},
		InputRequirements: registry.InputRequirements{
			"in": registry.StaticRequirements(&manifest.AudioConstraint{}),
			"ir": registry.StaticRequirements(&manifest.AudioConstraint{}),
		},
		ProfileRequirements: registry.ProfileRequirements{
			"ir": func(inputs media.StreamSet, _ media.CodecID, _ registry.Configuration) ([]manifest.Capability, error) {
				input, ok := inputs["in"]
				if !ok || input.Type != media.MediaAudio || input.Audio.SampleRate == 0 {
					return nil, fmt.Errorf("convolve requires an audio main input with a sample rate")
				}
				return []manifest.Capability{&manifest.AudioConstraint{SampleRates: manifest.IntConstraint{Values: []int{input.Audio.SampleRate}}}}, nil
			},
		},
		Resources: registry.ResourceRequest{Parallelism: true},
	}, OutputPorts: []string{"out"}, Factory: registry.SingleFactory(func(in media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
		value, err := engine.ResolveConfig[config.ConvolutionConfig, ConvolutionConfig](options.Config)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}
		item, err := convolve.New(value)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}
		if len(value.ImpulseResponse) != 0 {
			return engine.WrapFilter(item), in, nil
		}
		return engine.WrapFilter(item, engine.WithInputs(
			engine.FilterInput{ID: "in", Phase: node.InputPhaseRun},
			engine.FilterInput{ID: "ir", Phase: node.InputPhasePreload},
		)), in, nil
	})}); err != nil {
		panic(err)
	}
}
