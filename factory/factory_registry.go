package factory

import "github.com/godexture/core/registry"

type RegistryOption func(*registry.Bundle)

func WithMuxerRegistry(reg *registry.MuxerRegistry) RegistryOption {
	return func(b *registry.Bundle) {
		b.Muxers = reg
	}
}

func WithDemuxerRegistry(reg *registry.DemuxerRegistry) RegistryOption {
	return func(b *registry.Bundle) {
		b.Demuxers = reg
	}
}

func WithEncoderRegistry(reg *registry.EncoderRegistry) RegistryOption {
	return func(b *registry.Bundle) {
		b.Encoders = reg
	}
}

func WithDecoderRegistry(reg *registry.DecoderRegistry) RegistryOption {
	return func(b *registry.Bundle) {
		b.Decoders = reg
	}
}

func WithFilterRegistry(reg *registry.FilterRegistry) RegistryOption {
	return func(b *registry.Bundle) {
		b.Filters = reg
	}
}

func (f *Provider) NewRegistry(opts ...RegistryOption) registry.Bundle {
	bundle := f.defaultRegistry

	for _, opt := range opts {
		opt(&bundle)
	}

	return bundle
}
