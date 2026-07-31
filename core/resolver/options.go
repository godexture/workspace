package resolver

import (
	"reflect"

	"github.com/godexture/core/registry"
)

type Priority int

type ResolveOptions struct {
	priorityOverrides map[reflect.Type]Priority
}

type Option func(*ResolveOptions)

func WithPriority(config registry.Configuration, priority Priority) Option {
	return func(o *ResolveOptions) {
		if o.priorityOverrides == nil {
			o.priorityOverrides = make(map[reflect.Type]Priority)
		}
		configType := reflect.TypeOf(config)
		for configType != nil && configType.Kind() == reflect.Pointer {
			configType = configType.Elem()
		}
		o.priorityOverrides[configType] = priority
	}
}

func parseOptions(base *ResolveOptions, opts ...Option) *ResolveOptions {
	options := &ResolveOptions{}

	if base != nil {
		options.priorityOverrides = make(map[reflect.Type]Priority, len(base.priorityOverrides))
		for configType, priority := range base.priorityOverrides {
			options.priorityOverrides[configType] = priority
		}
	}

	for _, opt := range opts {
		opt(options)
	}
	return options
}

func (o *ResolveOptions) priority(key registry.PluginKey) Priority {
	return o.priorityOverrides[key.ConfigurationType()]
}
