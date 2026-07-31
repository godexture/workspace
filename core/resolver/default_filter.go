package resolver

import (
	"fmt"

	"github.com/godexture/core/registry"
)

type DefaultFilterResolver struct {
	registry *registry.FilterRegistry
}

func NewDefaultFilterResolver(reg *registry.FilterRegistry) *DefaultFilterResolver {
	return &DefaultFilterResolver{registry: reg}
}

func (r *DefaultFilterResolver) ResolveFilter(config registry.Configuration) (registry.FilterManifest, error) {
	if config == nil {
		return registry.FilterManifest{}, fmt.Errorf("filter configuration must not be nil")
	}
	key, err := r.registry.Key(config)
	if err != nil {
		return registry.FilterManifest{}, fmt.Errorf("invalid filter configuration: %w", err)
	}
	result, err := r.registry.Get(key)
	if err != nil {
		return result, fmt.Errorf("filter not found for configuration %T: %w", config, err)
	}
	return result, nil
}
