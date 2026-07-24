package registry

import (
	"fmt"
	"iter"
	"slices"
)

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
