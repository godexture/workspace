package registry

import (
	"reflect"
	"sync"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/internal/xsync"
)

type Manifest interface {
	ID() PluginKey
	RegistryName() string
	ConfigurationType() reflect.Type
	NewConfiguration() (Configuration, error)
	Default() Configuration
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
