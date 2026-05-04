package resolver

import (
	"reflect"

	"github.com/godexture/core/registry"
)

type Priority int

type ResolveOptions struct {
	PriorityOverrides map[reflect.Type]Priority
}

type Option func(*ResolveOptions)

func WithPriority(config registry.Configuration, priority Priority) Option {
	return func(o *ResolveOptions) {
		if o.PriorityOverrides == nil {
			o.PriorityOverrides = make(map[reflect.Type]Priority)
		}

		o.PriorityOverrides[reflect.TypeOf(config)] = priority
	}
}

func parseOptions(base *ResolveOptions, opts ...Option) *ResolveOptions {
	options := &ResolveOptions{}

	if base != nil {
		options.PriorityOverrides = base.PriorityOverrides
	}

	for _, opt := range opts {
		opt(options)
	}
	return options
}
