package resolver

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

type DefaultDecoderResolver struct {
	registry    *registry.Registry[registry.DecoderManifest]
	baseOptions *ResolveOptions
}

func NewDefaultDecoderResolver(reg *registry.Registry[registry.DecoderManifest], opts ...Option) *DefaultDecoderResolver {
	return &DefaultDecoderResolver{
		registry:    reg,
		baseOptions: parseOptions(nil, opts...),
	}
}

func (r *DefaultDecoderResolver) ResolveDecoder(stream media.StreamInfo, opts ...Option) (registry.DecoderManifest, error) {
	options := parseOptions(r.baseOptions, opts...)

	var bestManifest registry.DecoderManifest
	var maxPriority Priority = -1

	for manifest := range r.registry.Enumerate() {
		accepted, err := manifest.Accept(stream, stream.Codec, nil)
		if err != nil {
			continue
		}
		if accepted {
			priority := options.priority(manifest.ID())
			if priority > maxPriority {
				maxPriority = priority
				bestManifest = manifest
			}
		}
	}

	if maxPriority < 0 {
		return bestManifest, fmt.Errorf("no decoder found for codec: %s", stream.Codec)
	}

	return bestManifest, nil
}
