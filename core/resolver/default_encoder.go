package resolver

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
)

type DefaultEncoderResolver struct {
	registry    *registry.Registry[registry.EncoderManifest]
	baseOptions *ResolveOptions
}

func NewDefaultEncoderResolver(reg *registry.Registry[registry.EncoderManifest], opts ...Option) *DefaultEncoderResolver {
	return &DefaultEncoderResolver{
		registry:    reg,
		baseOptions: parseOptions(nil, opts...),
	}
}

func (r *DefaultEncoderResolver) ResolveEncoder(codec media.CodecID, opts ...Option) (registry.EncoderManifest, error) {
	options := parseOptions(r.baseOptions, opts...)

	var bestManifest registry.EncoderManifest
	var maxPriority Priority = -1

	for manifest := range r.registry.Enumerate() {
		if manifest.Supports(codec) {
			priority := options.priority(manifest.ID())
			if priority > maxPriority {
				maxPriority = priority
				bestManifest = manifest
			}
		}
	}

	if maxPriority < 0 {
		return bestManifest, fmt.Errorf("no encoder found for codec %s", codec)
	}

	return bestManifest, nil
}
