package engine

import (
	"fmt"
	"log"

	"github.com/godexture/core/registry"
)

type Validateable interface {
	Validate() error
}

type Wrapper[T any] interface {
	registry.Configuration
	Resolve() T
	ResolveDefault() T
}

// ResolveConfig resolves a configuration from the registry.
// It accepts either a value type T or a pointer type *T.
func ResolveConfig[T any, C Wrapper[T]](cfg registry.Configuration) (T, error) {
	var resolved T

	if cfg != nil {
		if v, ok := cfg.(C); ok {
			log.Printf("ResolveConfig: cfg is of type %T, resolved type %T", v, resolved)
			resolved = v.Resolve()
		} else if p, ok := any(cfg).(*C); ok && p != nil {
			log.Printf("ResolveConfig: cfg is of type %T, resolved type %T", *p, resolved)
			resolved = (*p).Resolve()
		}
	} else {
		var wrapper C
		resolved = wrapper.ResolveDefault()
	}

	if validateable, ok := any(resolved).(Validateable); ok {
		if err := validateable.Validate(); err != nil {
			return resolved, fmt.Errorf("invalid configuration: %w", err)
		}
	}
	return resolved, nil
}
