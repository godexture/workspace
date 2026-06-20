package factory

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type ResolverOption func(*resolver.Bundle)

func WithMuxerResolver(res resolver.MuxerResolver) ResolverOption {
	return func(b *resolver.Bundle) {
		b.NewMuxerResolver = func(*registry.MuxerRegistry) resolver.MuxerResolver {
			return res
		}
	}
}

func WithDemuxerResolver(res resolver.DemuxerResolver) ResolverOption {
	return func(b *resolver.Bundle) {
		b.NewDemuxerResolver = func(*registry.DemuxerRegistry) resolver.DemuxerResolver {
			return res
		}
	}
}

func WithEncoderResolver(res resolver.EncoderResolver) ResolverOption {
	return func(b *resolver.Bundle) {
		b.NewEncoderResolver = func(*registry.EncoderRegistry) resolver.EncoderResolver {
			return res
		}
	}
}

func WithDecoderResolver(res resolver.DecoderResolver) ResolverOption {
	return func(b *resolver.Bundle) {
		b.NewDecoderResolver = func(*registry.DecoderRegistry) resolver.DecoderResolver {
			return res
		}
	}
}

func (f *Provider) NewResolver(opts ...ResolverOption) resolver.Bundle {
	bundle := f.defaultResolver

	for _, opt := range opts {
		opt(&bundle)
	}

	return bundle
}
