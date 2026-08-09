package catalog

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type accessScheme struct {
	direction string
	scheme    string
}

func validateTraits(components []plugin.Component) []diagnostic.Item {
	seen := make(map[accessScheme]plugin.Identity)
	var items []diagnostic.Item
	for _, component := range components {
		shape := component.Ports()
		if trait, ok := access.SourceOf(component); ok {
			items = append(items, validateSourceTrait(component, shape, trait, seen)...)
		}
		if trait, ok := access.SinkOf(component); ok {
			items = append(items, validateSinkTrait(component, shape, trait, seen)...)
		}
		if trait, ok := endpoint.TraitOf(component); ok {
			if !trait.Valid() {
				items = append(items, traitItem("catalog.endpoint-trait", component.Identity(), "Endpoint trait is invalid", nil))
			}
			_, source := bound.Port(shape, plan.InputBoundary)
			_, sink := bound.Port(shape, plan.OutputBoundary)
			if !source && !sink {
				items = append(items, traitItem("catalog.endpoint-shape", component.Identity(), "Endpoint trait requires one directional port", nil))
			}
		}
	}
	return items
}

func validateSourceTrait(component plugin.Component, shape flow.Shape, trait access.SourceTrait, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.access-trait", component.Identity(), "Access source trait is invalid", map[string]string{"direction": "source", "scheme": trait.Scheme()}))
	}
	if _, ok := bound.Port(shape, plan.InputBoundary); !ok {
		items = append(items, traitItem("catalog.access-shape", component.Identity(), "Access source trait requires a 0-input/1-output component", map[string]string{"direction": "source", "scheme": trait.Scheme()}))
	}
	return append(items, validateScheme(component.Identity(), "source", trait.Scheme(), trait.Valid(), seen)...)
}

func validateSinkTrait(component plugin.Component, shape flow.Shape, trait access.SinkTrait, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.access-trait", component.Identity(), "Access sink trait is invalid", map[string]string{"direction": "sink", "scheme": trait.Scheme()}))
	}
	if _, ok := bound.Port(shape, plan.OutputBoundary); !ok {
		items = append(items, traitItem("catalog.access-shape", component.Identity(), "Access sink trait requires a 1-input/0-output component", map[string]string{"direction": "sink", "scheme": trait.Scheme()}))
	}
	return append(items, validateScheme(component.Identity(), "sink", trait.Scheme(), trait.Valid(), seen)...)
}

func validateScheme(identity plugin.Identity, direction, scheme string, valid bool, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	if !valid {
		return nil
	}
	key := accessScheme{direction: direction, scheme: scheme}
	previous, exists := seen[key]
	if !exists {
		seen[key] = identity
		return nil
	}
	return []diagnostic.Item{traitItem("catalog.access-scheme", identity, "Access scheme is repeated for one direction", map[string]string{
		"direction": direction,
		"scheme":    scheme,
		"previous":  previous.String(),
	})}
}

func traitItem(code string, component plugin.Identity, message string, detail map[string]string) diagnostic.Item {
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail)
}
