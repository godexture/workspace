package core

import (
	"github.com/godexture/godec/core/factory"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/resolver"
)

var (
	DefaultMuxerRegistry               = registry.NewRegistry[registry.MuxerManifest]()
	DefaultDemuxerRegistry             = registry.NewRegistry[registry.DemuxerManifest]()
	DefaultEncoderRegistry             = registry.NewRegistry[registry.EncoderManifest]()
	DefaultDecoderRegistry             = registry.NewRegistry[registry.DecoderManifest]()
	DefaultFilterRegistry              = registry.NewRegistry[registry.FilterManifest]()
	DefaultParameterizedFilterRegistry = registry.NewRegistry[registry.ParameterizedFilterManifest]()
)

var DefaultRegistry = registry.Bundle{
	Muxers:               DefaultMuxerRegistry,
	Demuxers:             DefaultDemuxerRegistry,
	Encoders:             DefaultEncoderRegistry,
	Decoders:             DefaultDecoderRegistry,
	Filters:              DefaultFilterRegistry,
	ParameterizedFilters: DefaultParameterizedFilterRegistry,
}

var f = factory.NewProvider(DefaultRegistry, resolver.Default)

var (
	NewRegistry   = f.NewRegistry
	NewResolver   = f.NewResolver
	NewNegotiator = f.NewNegotiator
	NewBuilder    = pipeline.NewBuilder
)

func Register(manifest registry.Manifest) error {
	return DefaultRegistry.Register(manifest)
}
