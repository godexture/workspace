package resolver

import (
	"fmt"

	"github.com/godexture/core/registry"
)

type DefaultMuxerResolver struct {
	registry *registry.Registry[registry.MuxerManifest]
}

func NewDefaultMuxerResolver(reg *registry.Registry[registry.MuxerManifest]) *DefaultMuxerResolver {
	return &DefaultMuxerResolver{
		registry: reg,
	}
}

func (r *DefaultMuxerResolver) ResolveMuxer(config registry.Configuration) (registry.MuxerManifest, error) {
	if config == nil {
		return registry.MuxerManifest{}, fmt.Errorf("muxer profile not specified")
	}

	key, err := r.registry.Key(config)
	if err != nil {
		return registry.MuxerManifest{}, fmt.Errorf("invalid muxer configuration: %w", err)
	}
	manifest, err := r.registry.Get(key)
	if err != nil {
		return manifest, fmt.Errorf("muxer not found for configuration %T: %w", config, err)
	}

	return manifest, nil
}
