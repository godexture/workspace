package registry

import (
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
