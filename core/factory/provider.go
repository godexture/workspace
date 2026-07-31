package factory

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/resolver"
)

type Provider struct {
	defaultRegistry registry.Bundle
	defaultResolver resolver.Bundle
}

func NewProvider(defaultRegistry registry.Bundle, defaultResolver resolver.Bundle) *Provider {
	return &Provider{
		defaultRegistry: defaultRegistry,
		defaultResolver: defaultResolver,
	}
}
