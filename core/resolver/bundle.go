package resolver

import "github.com/godexture/core/registry"

type Bundle struct {
	NewMuxerResolver   func(*registry.MuxerRegistry) MuxerResolver
	NewDemuxerResolver func(*registry.DemuxerRegistry) DemuxerResolver
	NewEncoderResolver func(*registry.EncoderRegistry) EncoderResolver
	NewDecoderResolver func(*registry.DecoderRegistry) DecoderResolver
}
