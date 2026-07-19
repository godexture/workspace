package xsync

import (
	"iter"
	"sync"
)

type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		m: make(map[K]V),
	}
}

func (m *Map[K, V]) Load(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.m[key]
	return v, ok
}

func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[key] = value
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	m.mu.RLock()
	if v, ok := m.m[key]; ok {
		m.mu.RUnlock()
		return v, true
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if v, ok := m.m[key]; ok {
		return v, true
	}

	m.m[key] = value
	return value, false
}

func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		for k, v := range m.m {
			if !yield(k, v) {
				break
			}
		}
	}
}

func (m *Map[K, V]) Clone() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[K]V, len(m.m))
	for k, v := range m.m {
		snapshot[k] = v
	}

	return snapshot
}
