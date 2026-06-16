package resolver

import (
	"fmt"
	"reflect"

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

	t := reflect.TypeOf(config)

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	manifest, err := r.registry.Get(t)
	if err != nil {
		return manifest, fmt.Errorf("muxer not found for type: %s): %w", t.String(), err)
	}

	return manifest, nil
}
