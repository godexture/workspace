package plugin

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
)

// Component is an opaque M2 component definition. Port shape, Compile, Open,
// and runtime interfaces are intentionally added in later milestones.
type Component struct {
	identity   Identity
	plugin     Identity
	descriptor Descriptor
	schema     config.SchemaView
	aliases    []string
	provenance Provenance
	problems   []diagnostic.Item
}

type componentOptions struct {
	aliases    []string
	provenance Provenance
}

// ComponentOption changes non-identity component metadata.
type ComponentOption func(*componentOptions)

// Aliases adds non-unique display/search aliases.
func Aliases(values ...string) ComponentOption {
	return func(options *componentOptions) { options.aliases = append(options.aliases, values...) }
}

// WithProvenance attaches build provenance to a component.
func WithProvenance(value Provenance) ComponentOption {
	return func(options *componentOptions) { options.provenance = value }
}

// NewComponent constructs a typed component definition without package-level
// registration or initialization side effects.
func NewComponent[Marker any, C any](descriptor Descriptor, schema config.Schema[C], options ...ComponentOption) Component {
	identity, identityErr := identityOf[Marker]()
	componentOptionsValue := componentOptions{provenance: descriptor.Provenance}
	for _, option := range options {
		if option != nil {
			option(&componentOptionsValue)
		}
	}
	result := Component{
		identity:   identity,
		descriptor: descriptor,
		schema:     schema.View(),
		aliases:    append([]string(nil), componentOptionsValue.aliases...),
		provenance: componentOptionsValue.provenance,
	}
	if identityErr != nil {
		result.problems = append(result.problems, diagnostic.NewItem("plugin.marker", diagnostic.ErrorSeverity, diagnostic.Path{}, identityErr.Error(), nil))
	}
	seen := make(map[string]struct{}, len(result.aliases))
	for _, alias := range result.aliases {
		if !validAlias(alias) {
			result.problems = append(result.problems, diagnostic.NewItem("plugin.alias", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "alias"}, "component alias is invalid", nil))
		}
		if _, exists := seen[alias]; exists {
			result.problems = append(result.problems, diagnostic.NewItem("plugin.alias", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "alias"}, "component aliases must be unique within a component", nil))
		}
		seen[alias] = struct{}{}
	}
	return result
}

// Identity returns the marker-derived component identity.
func (c Component) Identity() Identity { return c.identity }

// PluginIdentity returns the parent plugin marker identity after Define.
func (c Component) PluginIdentity() Identity { return c.plugin }

// Descriptor returns component distribution metadata.
func (c Component) Descriptor() Descriptor { return c.descriptor }

// Schema returns the type-erased control-plane schema view.
func (c Component) Schema() config.SchemaView { return c.schema }

// Aliases returns a copy of component aliases.
func (c Component) Aliases() []string { return append([]string(nil), c.aliases...) }

// Provenance returns build provenance.
func (c Component) Provenance() Provenance { return c.provenance }

// Diagnostics returns definition-time diagnostics without exposing mutable
// storage. Schema and descriptor validation is performed here so a zero or
// adapter-created component is still rejected by Host construction.
func (c Component) Diagnostics() []diagnostic.Item {
	items := cloneItems(c.problems)
	componentPath := diagnostic.Path{}
	if !c.identity.IsZero() {
		componentPath.Component = c.identity.String()
	}
	items = append(items, c.descriptor.diagnostics(componentPath)...)
	if c.schema == nil {
		items = append(items, diagnostic.NewItem("plugin.schema", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "schema"}, "component config schema is required", nil))
	} else if !c.schema.Valid() {
		items = append(items, c.schema.Diagnostics()...)
	}
	for index, item := range items {
		if item.Path.Component == "" && !c.identity.IsZero() {
			items[index] = item.WithPath(item.Path.WithComponent(c.identity.String()))
		}
	}
	return items
}

// View returns a read-only component description for catalog surfaces.
func (c Component) View() ComponentView {
	return ComponentView{
		Identity:   c.identity,
		Plugin:     c.plugin,
		Descriptor: c.descriptor,
		Schema:     c.schema.Description(),
		Aliases:    c.Aliases(),
		Provenance: c.provenance,
	}
}

func (c Component) withPlugin(identity Identity) Component {
	c.plugin = identity
	c.aliases = append([]string(nil), c.aliases...)
	c.problems = cloneItems(c.problems)
	return c
}

func (c Component) clone() Component { return c.withPlugin(c.plugin) }

// ComponentView is an immutable, surface-facing component description.
type ComponentView struct {
	Identity   Identity
	Plugin     Identity
	Descriptor Descriptor
	Schema     config.SchemaDescription
	Aliases    []string
	Provenance Provenance
}

// Definition is a plugin composition returned by a plugin function such as
// Plugin() or Codec().
type Definition struct {
	identity   Identity
	descriptor Descriptor
	components []Component
	problems   []diagnostic.Item
}

// Define constructs a plugin definition from a marker identity and opaque
// components. It records invalid metadata for host-time aggregate reporting.
func Define[Marker any](descriptor Descriptor, components ...Component) Definition {
	identity, identityErr := identityOf[Marker]()
	result := Definition{
		identity:   identity,
		descriptor: descriptor,
		components: make([]Component, len(components)),
	}
	if identityErr != nil {
		result.problems = append(result.problems, diagnostic.NewItem("plugin.marker", diagnostic.ErrorSeverity, diagnostic.Path{}, identityErr.Error(), nil))
	}
	seen := make(map[Identity]struct{}, len(components))
	for index, component := range components {
		component = component.clone()
		component.plugin = identity
		result.components[index] = component
		if _, exists := seen[component.identity]; exists {
			result.problems = append(result.problems, diagnostic.NewItem("plugin.duplicate-component", diagnostic.ErrorSeverity, diagnostic.Path{Component: component.identity.String()}, "component identity is repeated in a plugin definition", nil))
		}
		seen[component.identity] = struct{}{}
	}
	if len(components) == 0 {
		result.problems = append(result.problems, diagnostic.NewItem("plugin.empty-definition", diagnostic.ErrorSeverity, diagnostic.Path{}, "plugin definition must contain at least one component", nil))
	}
	return result
}

// Identity returns the plugin marker identity.
func (d Definition) Identity() Identity { return d.identity }

// Descriptor returns plugin distribution metadata.
func (d Definition) Descriptor() Descriptor { return d.descriptor }

// Components returns copied component definitions.
func (d Definition) Components() []Component {
	result := make([]Component, len(d.components))
	for index, component := range d.components {
		result[index] = component.clone()
	}
	return result
}

// Diagnostics returns definition-time and current component diagnostics.
func (d Definition) Diagnostics() []diagnostic.Item {
	items := cloneItems(d.problems)
	pluginPath := diagnostic.Path{}
	if !d.identity.IsZero() {
		pluginPath.Component = d.identity.String()
	}
	items = append(items, d.descriptor.diagnostics(pluginPath)...)
	for _, component := range d.components {
		items = append(items, component.Diagnostics()...)
	}
	for index, item := range items {
		if item.Path.Component == "" && !d.identity.IsZero() {
			items[index] = item.WithPath(item.Path.WithComponent(d.identity.String()))
		}
	}
	return items
}

func (d Definition) clone() Definition {
	d.components = d.Components()
	d.problems = cloneItems(d.problems)
	return d
}

func (d Definition) replaceComponent(target Identity, replacement Component) (Definition, bool) {
	result := d.clone()
	for index, component := range result.components {
		if component.identity == target {
			replacement.plugin = result.identity
			result.components[index] = replacement.clone()
			return result, true
		}
	}
	return d, false
}

// DefinitionView is a read-only plugin description.
type DefinitionView struct {
	Identity   Identity
	Descriptor Descriptor
	Components []ComponentView
}

// View returns a read-only plugin description.
func (d Definition) View() DefinitionView {
	components := d.Components()
	views := make([]ComponentView, len(components))
	for index, component := range components {
		views[index] = component.View()
	}
	return DefinitionView{Identity: d.identity, Descriptor: d.descriptor, Components: views}
}

func cloneItems(items []diagnostic.Item) []diagnostic.Item {
	result := make([]diagnostic.Item, len(items))
	for index, item := range items {
		result[index] = item.WithPath(item.Path)
	}
	return result
}
