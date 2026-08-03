// Package catalog builds the private validated component index used by Host.
// It is intentionally internal: runtime selection state is not a public
// plugin contract.
package catalog

import (
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/plugin"
)

// Index is an immutable, validated component index.
type Index struct {
	components  []plugin.Component
	byID        map[plugin.Identity]int
	fingerprint [32]byte
}

// Build validates every definition and returns an index only when all entries
// are valid. Broken definitions are never silently omitted.
func Build(set plugin.Set) (Index, error) {
	items := set.Diagnostics()
	definitions := set.Definitions()
	seenPlugins := make(map[plugin.Identity]struct{}, len(definitions))
	for _, definition := range definitions {
		identity := definition.Identity()
		if !identity.IsZero() {
			if _, exists := seenPlugins[identity]; exists {
				items = append(items, diagnostic.NewItem("plugin.duplicate-identity", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "plugin identity is repeated", nil))
			}
			seenPlugins[identity] = struct{}{}
		}
		items = append(items, definition.Diagnostics()...)
		if identity.IsZero() {
			items = append(items, diagnostic.NewItem("catalog.plugin-identity", diagnostic.ErrorSeverity, diagnostic.Path{}, "plugin definition has no valid marker identity", nil))
		}
	}

	components := set.Components()
	seen := make(map[plugin.Identity]struct{}, len(components))
	for _, component := range components {
		identity := component.Identity()
		if identity.IsZero() {
			items = append(items, diagnostic.NewItem("catalog.component-identity", diagnostic.ErrorSeverity, diagnostic.Path{}, "component has no valid marker identity", nil))
			continue
		}
		if _, exists := seen[identity]; exists {
			items = append(items, diagnostic.NewItem("catalog.duplicate-component", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "component identity is repeated", nil))
			continue
		}
		// One marker cannot answer both "which plugin" and "which component":
		// Remove and Override would match two different things.
		if _, exists := seenPlugins[identity]; exists {
			items = append(items, diagnostic.NewItem("catalog.identity-conflict", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "marker identity is used by both a plugin and a component", nil))
			continue
		}
		if component.PluginIdentity().IsZero() {
			items = append(items, diagnostic.NewItem("catalog.plugin-identity", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "component has no parent plugin identity", nil))
		}
		seen[identity] = struct{}{}
	}

	if hasError(items) {
		return Index{}, diagnostic.NewError(items...)
	}
	// Set.Components is already sorted, but sort here as a defense against
	// future set adapters that do not preserve that property.
	sort.Slice(components, func(left, right int) bool {
		return components[left].Identity().String() < components[right].Identity().String()
	})
	byID := make(map[plugin.Identity]int, len(components))
	for index, component := range components {
		byID[component.Identity()] = index
		components[index] = component
	}
	return Index{
		components:  copyComponents(components),
		byID:        byID,
		fingerprint: catalogFingerprint(definitions, components),
	}, nil
}

// Components returns copied component definitions in stable identity order.
func (i Index) Components() []plugin.Component {
	return copyComponents(i.components)
}

// Views returns copied read-only component descriptions.
func (i Index) Views() []plugin.ComponentView {
	components := i.Components()
	views := make([]plugin.ComponentView, len(components))
	for index, component := range components {
		views[index] = component.View()
	}
	return views
}

// Lookup returns a copied component by marker identity.
func (i Index) Lookup(identity plugin.Identity) (plugin.Component, bool) {
	index, ok := i.byID[identity]
	if !ok {
		return plugin.Component{}, false
	}
	return i.components[index], true
}

// Len reports the number of validated components.
func (i Index) Len() int { return len(i.components) }

// Fingerprint returns the stable identity of this validated catalog.
func (i Index) Fingerprint() [32]byte { return i.fingerprint }

func copyComponents(components []plugin.Component) []plugin.Component {
	result := make([]plugin.Component, len(components))
	for index, component := range components {
		result[index] = component
	}
	return result
}

func hasError(items []diagnostic.Item) bool {
	for _, item := range items {
		if item.Severity == diagnostic.ErrorSeverity {
			return true
		}
	}
	return false
}
