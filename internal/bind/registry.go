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

type Registry struct {
	index     catalog.Index
	providers map[string]access.Provider
	endpoints map[plugin.Identity]endpoint.Trait
}

func NewRegistry(index catalog.Index, providers []access.Provider, endpoints []endpoint.Component) (Registry, error) {
	result := Registry{
		index:     index,
		providers: make(map[string]access.Provider, len(providers)),
		endpoints: make(map[plugin.Identity]endpoint.Trait, len(endpoints)),
	}
	var items []diagnostic.Item
	for _, provider := range providers {
		if !provider.Valid() {
			items = append(items, bindItem("bind.invalid-provider", plugin.Identity{}, "Access Provider manifest is invalid", nil))
			continue
		}
		if _, ok := index.Lookup(provider.Identity()); !ok {
			items = append(items, bindItem("bind.provider-target", provider.Identity(), "Access Provider component is not in the catalog", nil))
			continue
		}
		for _, scheme := range provider.Schemes() {
			if previous, exists := result.providers[scheme]; exists {
				items = append(items, bindItem("bind.duplicate-provider", provider.Identity(), "Access Provider scheme is repeated", map[string]string{
					"scheme":   scheme,
					"previous": previous.Identity().String(),
				}))
				continue
			}
			result.providers[scheme] = provider
		}
	}
	for _, declared := range endpoints {
		if !declared.Valid() {
			items = append(items, bindItem("bind.invalid-endpoint", plugin.Identity{}, "Endpoint manifest is invalid", nil))
			continue
		}
		identity := declared.Identity()
		component, ok := index.Lookup(identity)
		if !ok {
			items = append(items, bindItem("bind.endpoint-target", identity, "Endpoint component is not in the catalog", nil))
			continue
		}
		if !component.Ports().Equal(declared.PluginComponent().Ports()) || component.Schema().Description().Identity != declared.PluginComponent().Schema().Description().Identity {
			items = append(items, bindItem("bind.endpoint-definition", identity, "Endpoint manifest does not describe the catalog component", nil))
			continue
		}
		if _, exists := result.endpoints[identity]; exists {
			items = append(items, bindItem("bind.duplicate-endpoint", identity, "Endpoint component is repeated", nil))
			continue
		}
		result.endpoints[identity] = declared.Trait()
	}
	if hasErrors(items) {
		return Registry{}, diagnostic.NewError(items...)
	}
	return result, nil
}

func (r Registry) Valid() bool {
	return r.index.Len() != 0 || len(r.providers) == 0 && len(r.endpoints) == 0
}

func bindItem(code string, component plugin.Identity, message string, detail map[string]string) diagnostic.Item {
	path := diagnostic.Path{}
	if !component.IsZero() {
		path.Component = component.String()
	}
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail)
}

func hasErrors(items []diagnostic.Item) bool {
	for _, item := range items {
		if item.Severity == diagnostic.ErrorSeverity {
			return true
		}
	}
	return false
}
