package plugin

import (
	"sort"
	"strings"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
)

// Set is an immutable persistent collection of plugin definitions. Operations
// return a new Set and never mutate the receiver.
type Set struct {
	definitions []Definition
	problems    []diagnostic.Item
	bindings    []codec.Binding
}

// NewSet creates a set. Composition problems are retained until the set is
// consumed by a host.
func NewSet(definitions ...Definition) Set {
	set := Set{}
	for _, definition := range definitions {
		set = set.Add(definition)
	}
	return set
}

// Add returns a set containing definition. Plugin and component marker
// identities must be unique; invalid metadata remains in the set for Host to
// report as a broken definition rather than being silently dropped.
func (s Set) Add(definition Definition) Set {
	for _, existing := range s.definitions {
		if existing.identity == definition.identity && !definition.identity.IsZero() {
			return s.withProblem(duplicateItem(definition.identity, "plugin identity is repeated"))
		}
		for _, existingComponent := range existing.components {
			for _, component := range definition.components {
				if existingComponent.identity == component.identity && !component.identity.IsZero() {
					return s.withProblem(duplicateItem(component.identity, "component identity is repeated"))
				}
			}
		}
	}
	result := Set{
		definitions: make([]Definition, len(s.definitions)+1),
		problems:    cloneItems(s.problems),
		bindings:    append([]codec.Binding(nil), s.bindings...),
	}
	for index, existing := range s.definitions {
		result.definitions[index] = existing.clone()
	}
	result.definitions[len(s.definitions)] = definition.clone()
	return result
}

// AddBinding adds a composition binding. Conflicting keys are retained so
// host construction can report the complete conflict; it never applies an
// implicit last-wins rule.
func (s Set) AddBinding(binding codec.Binding) Set {
	result := Set{
		definitions: make([]Definition, len(s.definitions)),
		problems:    cloneItems(s.problems),
		bindings:    append(append([]codec.Binding(nil), s.bindings...), binding),
	}
	for index, definition := range s.definitions {
		result.definitions[index] = definition.clone()
	}
	return result
}

// Bindings returns the immutable composition bindings in registration order.
func (s Set) Bindings() []codec.Binding { return append([]codec.Binding(nil), s.bindings...) }

// OverrideBinding explicitly replaces all bindings for one key.
func (s Set) OverrideBinding(target format.Tag, replacement codec.Binding) Set {
	if !replacement.Valid() || replacement.Key() != target {
		return s.withProblem(diagnostic.NewItem("plugin.binding-override", diagnostic.ErrorSeverity, diagnostic.Path{}, "binding override key and replacement key must match", nil))
	}
	result := Set{problems: cloneItems(s.problems), bindings: make([]codec.Binding, 0, len(s.bindings)+1)}
	for _, definition := range s.definitions {
		result.definitions = append(result.definitions, definition.clone())
	}
	replaced := false
	for _, binding := range s.bindings {
		if binding.Key() == target {
			replaced = true
			continue
		}
		result.bindings = append(result.bindings, binding)
	}
	if !replaced {
		return s.withProblem(diagnostic.NewItem("plugin.binding-override", diagnostic.ErrorSeverity, diagnostic.Path{}, "binding override target is not present", nil))
	}
	result.bindings = append(result.bindings, replacement)
	return result
}

// Remove returns a new set with identity removed. A plugin identity removes
// the whole definition; a component identity removes only that component.
func (s Set) Remove(identity Identity) Set {
	result := Set{problems: cloneItems(s.problems), bindings: append([]codec.Binding(nil), s.bindings...)}
	removed := false
	for _, definition := range s.definitions {
		if definition.identity == identity {
			removed = true
			continue
		}
		components := definition.Components()
		filtered := components[:0]
		for _, component := range components {
			if component.identity == identity {
				removed = true
				continue
			}
			filtered = append(filtered, component)
		}
		if len(filtered) == 0 {
			if len(components) != 0 {
				removed = true
			}
			continue
		}
		definition.components = append([]Component(nil), filtered...)
		result.definitions = append(result.definitions, definition.clone())
	}
	if !removed {
		return s.withProblem(invalidCompositionItem("plugin.remove-missing", identity, "remove target identity is not present").
			WithSuggestions(diagnostic.Suggest(identity.String(), s.identityNames())))
	}
	return result
}

// Override replaces the component with target identity. The replacement must
// carry the same marker identity, making replacement explicit without
// allowing identity changes to masquerade as an override.
func (s Set) Override(target Identity, replacement Component) Set {
	if target.IsZero() || replacement.identity != target {
		return s.withProblem(invalidOverrideItem("override target and replacement component identities must match"))
	}
	result := Set{problems: cloneItems(s.problems), bindings: append([]codec.Binding(nil), s.bindings...)}
	replaced := false
	for _, definition := range s.definitions {
		updated, ok := definition.replaceComponent(target, replacement)
		if ok {
			replaced = true
		}
		result.definitions = append(result.definitions, updated)
	}
	if !replaced {
		return s.withProblem(invalidCompositionItem("plugin.override", target, "override target identity is not present").
			WithSuggestions(diagnostic.Suggest(target.String(), s.componentNames())))
	}
	return result
}

// OverridePlugin replaces a whole plugin definition by plugin identity.
func (s Set) OverridePlugin(target Identity, replacement Definition) Set {
	if target.IsZero() || replacement.identity != target {
		return s.withProblem(invalidOverrideItem("override target and replacement plugin identities must match"))
	}
	result := Set{problems: cloneItems(s.problems), bindings: append([]codec.Binding(nil), s.bindings...)}
	replaced := false
	for _, definition := range s.definitions {
		if definition.identity == target {
			result.definitions = append(result.definitions, replacement.clone())
			replaced = true
			continue
		}
		result.definitions = append(result.definitions, definition.clone())
	}
	if !replaced {
		return s.withProblem(invalidCompositionItem("plugin.override", target, "override target identity is not present").
			WithSuggestions(diagnostic.Suggest(target.String(), s.pluginNames())))
	}
	return result
}

// Diagnostics returns retained composition problems.
func (s Set) Diagnostics() []diagnostic.Item { return cloneItems(s.problems) }

// Definitions returns copied definitions in composition order.
func (s Set) Definitions() []Definition {
	result := make([]Definition, len(s.definitions))
	for index, definition := range s.definitions {
		result[index] = definition.clone()
	}
	return result
}

// Components returns copied components sorted by marker identity. Sorting
// makes catalog construction independent of registration order.
func (s Set) Components() []Component {
	var result []Component
	for _, definition := range s.definitions {
		result = append(result, definition.Components()...)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].identity.String() < result[right].identity.String() })
	return result
}

// Empty reports whether no plugin/component definition is present.
func (s Set) Empty() bool { return len(s.definitions) == 0 }

// pluginNames and componentNames list the identities a composition call could
// legitimately have targeted, so an unknown target reports alternatives rather
// than only stating that it was absent.
func (s Set) pluginNames() []string {
	names := make([]string, 0, len(s.definitions))
	for _, definition := range s.definitions {
		if !definition.identity.IsZero() {
			names = append(names, definition.identity.String())
		}
	}
	return names
}

func (s Set) componentNames() []string {
	var names []string
	for _, definition := range s.definitions {
		for _, component := range definition.components {
			if !component.identity.IsZero() {
				names = append(names, component.identity.String())
			}
		}
	}
	return names
}

// identityNames covers Remove, which accepts either a plugin or a component
// identity.
func (s Set) identityNames() []string {
	return append(s.pluginNames(), s.componentNames()...)
}

func (s Set) withProblem(item diagnostic.Item) Set {
	result := Set{
		definitions: make([]Definition, len(s.definitions)),
		problems:    cloneItems(s.problems),
		bindings:    append([]codec.Binding(nil), s.bindings...),
	}
	for index, definition := range s.definitions {
		result.definitions[index] = definition.clone()
	}
	result.problems = append(result.problems, item)
	return result
}

func duplicateItem(identity Identity, message string) diagnostic.Item {
	return diagnostic.NewItem(
		"plugin.duplicate-identity",
		diagnostic.ErrorSeverity,
		diagnostic.Path{Component: identity.String()},
		message,
		nil,
	)
}

func invalidOverrideItem(message string) diagnostic.Item {
	return diagnostic.NewItem("plugin.override", diagnostic.ErrorSeverity, diagnostic.Path{}, strings.TrimSpace(message), nil)
}

func invalidCompositionItem(code string, identity Identity, message string) diagnostic.Item {
	path := diagnostic.Path{}
	if !identity.IsZero() {
		path.Component = identity.String()
	}
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, nil)
}
