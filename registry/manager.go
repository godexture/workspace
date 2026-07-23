package registry

import (
	"fmt"
	"iter"
	"slices"
	"sync"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/internal/xsync"
)

type Manifest interface {
	ID() PluginKey
	RegistryName() string
	NewConfiguration() (Configuration, error)
}

type Registry[V Manifest] struct {
	role       manifest.NodeType
	items      *xsync.Map[PluginKey, V]
	names      *xsync.Map[string, PluginKey]
	registerMu sync.Mutex
}

func NewRegistry[V Manifest]() *Registry[V] {
	return &Registry[V]{
		role:  manifestRole[V](),
		items: xsync.NewMap[PluginKey, V](),
		names: xsync.NewMap[string, PluginKey](),
	}
}

func (r *Registry[V]) Register(manifest V) error {
	config, err := manifest.NewConfiguration()
	if err != nil {
		return fmt.Errorf("create configuration: %w", err)
	}
	key, err := pluginKey(r.role, config)
	if err != nil {
		return fmt.Errorf("derive plugin key: %w", err)
	}
	manifest, err = assignManifestID(manifest, key)
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

	r.registerMu.Lock()
	defer r.registerMu.Unlock()
	if _, exists := r.items.Load(key); exists {
		return fmt.Errorf("plugin already registered: %s", key)
	}
	if _, exists := r.names.Load(manifest.RegistryName()); exists {
		return fmt.Errorf("plugin name already registered: %s", manifest.RegistryName())
	}
	r.items.Store(key, manifest)
	r.names.Store(manifest.RegistryName(), key)

	return nil
}

func assignManifestID[V Manifest](manifest V, key PluginKey) (V, error) {
	switch m := any(manifest).(type) {
	case BaseManifest:
		m.key = key
		return any(m).(V), nil

	case TransformManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case MuxerManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case DemuxerManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case EncoderManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case DecoderManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case FilterManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case ParameterizedFilterManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	default:
		return manifest, fmt.Errorf("invalid manifest type: %T", manifest)
	}
}

func manifestRole[V Manifest]() manifest.NodeType {
	var value V
	switch any(value).(type) {
	case MuxerManifest:
		return manifest.RoleMuxer
	case DemuxerManifest:
		return manifest.RoleDemuxer
	case EncoderManifest:
		return manifest.RoleEncoder
	case DecoderManifest:
		return manifest.RoleDecoder
	case FilterManifest:
		return manifest.RoleFilter
	case ParameterizedFilterManifest:
		return manifest.RoleFilter
	default:
		panic(fmt.Sprintf("unsupported registry manifest type: %T", value))
	}
}

func (r *Registry[V]) Key(config Configuration) (PluginKey, error) {
	return pluginKey(r.role, config)
}

func (r *Registry[V]) Get(key PluginKey) (V, error) {
	item, exists := r.items.Load(key)
	if !exists {
		return item, fmt.Errorf("plugin not found: %s", key)
	}
	return item, nil
}

func (r *Registry[V]) Lookup(name string) (V, error) {
	key, exists := r.names.Load(name)
	if !exists {
		var zero V
		return zero, fmt.Errorf("plugin not found: %s", name)
	}
	return r.Get(key)
}

// hasName reports whether name is already registered in r. It exists so
// Bundle.Register can enforce name uniqueness across sibling registries
// that share a namespace (Filters and ParameterizedFilters both resolve
// from the same "--filter name..." CLI spelling). A nil receiver (a Bundle
// that leaves this sibling registry unset, e.g. in isolated tests) safely
// reports no collision rather than panicking.
func (r *Registry[V]) hasName(name string) bool {
	if r == nil {
		return false
	}
	_, exists := r.names.Load(name)
	return exists
}

func (r *Registry[V]) Names() []string {
	snapshot := r.names.Clone()
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (r *Registry[V]) Enumerate() iter.Seq[V] {
	snapshot := r.items.Clone()
	keys := make([]PluginKey, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b PluginKey) int {
		switch {
		case a.String() < b.String():
			return -1
		case a.String() > b.String():
			return 1
		default:
			return 0
		}
	})

	return func(yield func(V) bool) {
		for _, key := range keys {
			if !yield(snapshot[key]) {
				return
			}
		}
	}
}
