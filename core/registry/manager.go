// ============================================================================
// File: registry/format.go
// 役割: プラグインの自己登録機構と依存性解決（DI）
// ============================================================================
package registry

import (
	"fmt"
	"iter"
	"reflect"

	"github.com/godexture/core/internal/xsync"
)

type Manifest interface {
	ID() reflect.Type
}

type Registry[V Manifest] struct {
	items *xsync.Map[reflect.Type, V]
}

func NewRegistry[V Manifest]() *Registry[V] {
	return &Registry[V]{
		items: xsync.NewMap[reflect.Type, V](),
	}
}

func (r *Registry[V]) Register(config Configration, manifest V) error {
	if manifest, ok := any(manifest).(BaseManifest); ok {
		manifest.id = reflect.TypeOf(config)
	} else {
		return fmt.Errorf("invalid manifest type: %T", manifest)
	}

	if defaulter, ok := any(manifest).(Defaulter); ok {
		defaulter.ApplyDefaults()
	}

	if validator, ok := any(manifest).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid manifest: %w", err)
		}
	}

	r.items.Store(manifest.ID(), manifest)

	return nil
}

func (r *Registry[V]) Get(id reflect.Type) (V, error) {
	item, exists := r.items.Load(id)
	if !exists {
		return item, fmt.Errorf("plugin not found: %s", id)
	}
	return item, nil
}

func (r *Registry[V]) Enumerate() iter.Seq[V] {
	return xsync.EnumerateMapValues[V](r.items)
}
