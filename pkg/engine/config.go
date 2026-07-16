package engine

import "github.com/godexture/core/registry"

// ConfigWrapper represents a generated configuration type that can apply its defaults.
type ConfigWrapper[T any] interface {
	ApplyDefaults() T
}

// ResolveConfig resolves a configuration from the registry and applies its defaults.
// It accepts either a value type W or a pointer type *W that implements ConfigWrapper[T].
func ResolveConfig[W ConfigWrapper[T], T any](cfg registry.Configuration) T {
	var wrapper W
	if cfg != nil {
		if v, ok := cfg.(W); ok {
			wrapper = v
		} else if p, ok := any(cfg).(*W); ok && p != nil {
			wrapper = *p
		}
	}
	return wrapper.ApplyDefaults()
}
