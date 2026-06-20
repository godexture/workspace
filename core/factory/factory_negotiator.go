package factory

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
	"github.com/godexture/core/routing"
)

type NegotiatorConfig struct {
	Registry registry.Bundle
	Resolver resolver.Bundle
}

type NegotiatorOption func(*NegotiatorConfig)

func WithRegistry(reg registry.Bundle) NegotiatorOption {
	return func(c *NegotiatorConfig) {
		c.Registry = reg
	}
}

func WithResolver(res resolver.Bundle) NegotiatorOption {
	return func(c *NegotiatorConfig) {
		c.Resolver = res
	}
}

func (f *Provider) NewNegotiator(opts ...NegotiatorOption) *routing.Negotiator {
	config := &NegotiatorConfig{
		Registry: f.NewRegistry(),
		Resolver: f.NewResolver(),
	}

	for _, opt := range opts {
		opt(config)
	}

	return routing.NewNegotiator(
		config.Resolver.NewMuxerResolver(config.Registry.Muxers),
		config.Resolver.NewDemuxerResolver(config.Registry.Demuxers),
		config.Resolver.NewEncoderResolver(config.Registry.Encoders),
		config.Resolver.NewDecoderResolver(config.Registry.Decoders),
	)
}
