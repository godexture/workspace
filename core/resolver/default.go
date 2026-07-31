package resolver

import "github.com/godexture/core/registry"

var Default = Bundle{
	NewMuxerResolver: func(reg *registry.MuxerRegistry) MuxerResolver {
		return NewDefaultMuxerResolver(reg)
	},
	NewDemuxerResolver: func(reg *registry.DemuxerRegistry) DemuxerResolver {
		return NewDefaultDemuxerResolver(reg)
	},
	NewEncoderResolver: func(reg *registry.EncoderRegistry) EncoderResolver {
		return NewDefaultEncoderResolver(reg)
	},
	NewDecoderResolver: func(reg *registry.DecoderRegistry) DecoderResolver {
		return NewDefaultDecoderResolver(reg)
	},
	NewFilterResolver: func(reg *registry.FilterRegistry) FilterResolver {
		return NewDefaultFilterResolver(reg)
	},
	NewBridgeResolver: func(reg *registry.FilterRegistry) BridgeResolver {
		return NewDefaultBridgeResolver(reg)
	},
}
