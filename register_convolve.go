package filter

import (
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
	register(registry.NewConfigurationFactory(NewConvolutionConfig), "convolve", "Apply FFT convolution against an impulse response supplied via the Go API", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.ConvolutionConfig, ConvolutionConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := convolve.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
