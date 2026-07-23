package filter

import (
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
	}, Factory: func(in media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
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
		return engine.WrapMultiFilter(item, engine.FilterInput{ID: "in", Phase: node.InputPhaseRun}, engine.FilterInput{ID: "ir", Phase: node.InputPhasePreload}), in, nil
	}}); err != nil {
		panic(err)
	}
}
