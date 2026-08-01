package filter

import (
	"fmt"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/convolver"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{
		BaseManifest: registry.BaseManifest{Name: "convolver", Description: "Apply FFT convolution against an impulse response", ConfigurationFactory: registry.NewConfigurationFactory(NewConvolutionConfig)},
		InputRequirements: registry.InputRequirements{
			"in": registry.StaticRequirements(&manifest.AudioConstraint{}),
			"ir": registry.StaticRequirements(&manifest.AudioConstraint{}),
		},
		ProfileRequirements: registry.ProfileRequirements{
			"ir": func(inputs media.StreamSet, _ media.CodecID, _ registry.Configuration) ([]manifest.Capability, error) {
				input, ok := inputs["in"]
				if !ok || input.Type != media.MediaAudio || input.Audio.SampleRate == 0 {
					return nil, fmt.Errorf("convolver requires an audio main input with a sample rate")
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
		item, err := convolver.New(value)
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
