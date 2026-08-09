// Package bind normalizes Access Provider and Endpoint Job choices into a
// typed requested graph without opening resources or component operators.
package bind

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/plugin"
)

type sourceBinding struct {
	component plugin.Identity
	trait     access.SourceTrait
}

type sinkBinding struct {
	component plugin.Identity
	trait     access.SinkTrait
}

type Registry struct {
	index     catalog.Index
	sources   map[string]sourceBinding
	sinks     map[string]sinkBinding
	endpoints map[plugin.Identity]endpoint.Trait
}

func NewRegistry(index catalog.Index) Registry {
	result := Registry{
		index:     index,
		sources:   make(map[string]sourceBinding),
		sinks:     make(map[string]sinkBinding),
		endpoints: make(map[plugin.Identity]endpoint.Trait),
	}
	for _, component := range index.Components() {
		if trait, ok := access.SourceOf(component); ok && trait.Valid() {
			result.sources[trait.Scheme()] = sourceBinding{component: component.Identity(), trait: trait}
		}
		if trait, ok := access.SinkOf(component); ok && trait.Valid() {
			result.sinks[trait.Scheme()] = sinkBinding{component: component.Identity(), trait: trait}
		}
		if trait, ok := endpoint.TraitOf(component); ok && trait.Valid() {
			result.endpoints[component.Identity()] = trait
		}
	}
	return result
}

func (r Registry) Valid() bool {
	return r.index.Len() != 0 || len(r.sources) == 0 && len(r.sinks) == 0 && len(r.endpoints) == 0
}

func bindItem(code string, component plugin.Identity, message string, detail map[string]string) diagnostic.Item {
	path := diagnostic.Path{}
	if !component.IsZero() {
		path.Component = component.String()
	}
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail)
}
