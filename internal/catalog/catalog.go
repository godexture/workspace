// Package catalog builds the private validated component index used by Host.
// It is intentionally internal: runtime selection state is not a public
// plugin contract.
package catalog

import (
	"reflect"
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/gotype"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

// Index is an immutable, validated component index.
type Index struct {
	components    []plugin.Component
	declarations  []plugin.Declaration
	byID          map[plugin.Identity]int
	byDeclaration map[plugin.DeclarationKey]int
	fingerprint   [32]byte
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
	seenComponents := make(map[plugin.Identity]struct{}, len(components))
	componentsByID := make(map[plugin.Identity]plugin.Component, len(components))
	type schemaUse struct {
		payload   reflect.Type
		component plugin.Identity
		port      string
	}
	seenSchemas := make(map[string]schemaUse)
	for _, component := range components {
		identity := component.Identity()
		if identity.IsZero() {
			items = append(items, diagnostic.NewItem("catalog.component-identity", diagnostic.ErrorSeverity, diagnostic.Path{}, "component has no valid marker identity", nil))
			continue
		}
		if _, exists := seenComponents[identity]; exists {
			items = append(items, diagnostic.NewItem("catalog.duplicate-component", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "component identity is repeated", nil))
			continue
		}
		if _, exists := seenPlugins[identity]; exists {
			items = append(items, diagnostic.NewItem("catalog.identity-conflict", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "marker identity is used by both a plugin and a component", nil))
			continue
		}
		if component.PluginIdentity().IsZero() {
			items = append(items, diagnostic.NewItem("catalog.plugin-identity", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String()}, "component has no parent plugin identity", nil))
		}
		seenComponents[identity] = struct{}{}
		componentsByID[identity] = component
		shape := component.Ports()
		for _, port := range append(append([]flow.Port(nil), shape.Inputs...), shape.Outputs...) {
			descriptor := port.Schema()
			if !descriptor.Valid() {
				continue
			}
			key := descriptor.Identity().String()
			previous, exists := seenSchemas[key]
			if exists && previous.payload != descriptor.Payload() {
				items = append(items, diagnostic.NewItem("catalog.schema-conflict", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String(), Descriptor: port.ID()}, "schema identity is bound to different Go payload types", map[string]string{
					"identity":          key,
					"previousComponent": previous.component.String(),
					"previousPort":      previous.port,
					"previousPayload":   gotype.Canonical(previous.payload),
					"payload":           gotype.Canonical(descriptor.Payload()),
				}))
				continue
			}
			if !exists {
				seenSchemas[key] = schemaUse{payload: descriptor.Payload(), component: identity, port: port.ID()}
			}
		}
	}
	items = append(items, validateTraits(components)...)

	declarations := set.Declarations()
	seenDeclarations := make(map[plugin.DeclarationKey]plugin.Declaration, len(declarations))
	normalizedDeclarations := make([]plugin.Declaration, 0, len(declarations))
	for _, declaration := range declarations {
		if !declaration.Valid() {
			message := "composition declaration is invalid"
			if problem := declaration.Problem(); problem != nil {
				message = problem.Error()
			}
			items = append(items, diagnostic.NewItem("catalog.invalid-declaration", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: declaration.Key().String()}, message, nil))
			continue
		}
		key := declaration.Key()
		previous, exists := seenDeclarations[key]
		if exists && !previous.SameTargets(declaration) {
			items = append(items, diagnostic.NewItem("catalog.declaration-conflict", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: key.String()}, "composition key has different declaration targets", nil))
		}
		for _, target := range declaration.Targets() {
			component, componentTarget := target.Component()
			if componentTarget {
				if _, present := seenComponents[component]; !present {
					items = append(items, diagnostic.NewItem("catalog.declaration-target", diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String(), Descriptor: key.String()}, "composition declaration target is not in the catalog", map[string]string{"target": component.String()}))
				}
			}
		}
		if metadata.IsBindingKey(key) {
			targets := declaration.Targets()
			if len(targets) != 1 {
				items = append(items, diagnostic.NewItem("catalog.metadata-binding", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: key.String()}, "metadata binding must name exactly one encoding component", nil))
			} else if identity, componentTarget := targets[0].Component(); componentTarget {
				if component, present := componentsByID[identity]; present {
					encoding, ok := metadata.EncodingOf(component)
					if !ok || !encoding.Valid() {
						items = append(items, diagnostic.NewItem("catalog.metadata-encoding", diagnostic.ErrorSeverity, diagnostic.Path{Component: identity.String(), Descriptor: key.String()}, "metadata binding target has no valid Encoding trait", nil))
					}
				}
			}
		}
		if exists {
			continue
		}
		seenDeclarations[key] = declaration
		normalizedDeclarations = append(normalizedDeclarations, declaration)
	}
	if hasError(items) {
		return Index{}, diagnostic.NewError(items...)
	}
	declarations = normalizedDeclarations

	sort.Slice(declarations, func(left, right int) bool {
		if declarations[left].Key() != declarations[right].Key() {
			return declarations[left].Key().String() < declarations[right].Key().String()
		}
		leftTargets := declarations[left].Targets()
		rightTargets := declarations[right].Targets()
		for index := 0; index < len(leftTargets) && index < len(rightTargets); index++ {
			if leftTargets[index] != rightTargets[index] {
				return leftTargets[index].String() < rightTargets[index].String()
			}
		}
		return len(leftTargets) < len(rightTargets)
	})

	sort.Slice(components, func(left, right int) bool {
		return components[left].Identity().String() < components[right].Identity().String()
	})
	byID := make(map[plugin.Identity]int, len(components))
	for index, component := range components {
		byID[component.Identity()] = index
	}
	byDeclaration := make(map[plugin.DeclarationKey]int, len(declarations))
	for index, declaration := range declarations {
		byDeclaration[declaration.Key()] = index
	}
	return Index{
		components:    copyComponents(components),
		declarations:  append([]plugin.Declaration(nil), declarations...),
		byID:          byID,
		byDeclaration: byDeclaration,
		fingerprint:   catalogFingerprint(definitions, components, declarations),
	}, nil
}

func (i Index) Declarations() []plugin.Declaration {
	return append([]plugin.Declaration(nil), i.declarations...)
}

// LookupDeclaration returns one validated composition declaration by key.
func (i Index) LookupDeclaration(key plugin.DeclarationKey) (plugin.Declaration, bool) {
	index, ok := i.byDeclaration[key]
	if !ok {
		return plugin.Declaration{}, false
	}
	return i.declarations[index], true
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
