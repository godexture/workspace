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

func (r *Registry[V]) Register(config Configuration, manifest V) error {
	var err error
	manifest, err = assignManifestID(manifest, reflect.TypeOf(config))
	if err != nil {
		return err
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

func assignManifestID[V Manifest](manifest V, id reflect.Type) (V, error) {
	switch m := any(manifest).(type) {
	case BaseManifest:
		m.id = id
		return any(m).(V), nil

	case TransformManifest:
		m.BaseManifest.id = id
		return any(m).(V), nil

	case MuxerManifest:
		m.BaseManifest.id = id
		return any(m).(V), nil

	case DemuxerManifest:
		m.BaseManifest.id = id
		return any(m).(V), nil

	case EncoderManifest:
		m.TransformManifest.BaseManifest.id = id
		return any(m).(V), nil

	case DecoderManifest:
		m.TransformManifest.BaseManifest.id = id
		return any(m).(V), nil

	case FilterManifest:
		m.TransformManifest.BaseManifest.id = id
		return any(m).(V), nil

	default:
		return manifest, fmt.Errorf("invalid manifest type: %T", manifest)
	}
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
