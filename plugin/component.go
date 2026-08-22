package plugin

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
)

// Component is a heterogeneous component definition. Its typed Spec is erased
// once by WithSpec and recovered only at Compile/Suggest/Open boundaries.
type Component struct {
	identity       Identity
	plugin         Identity
	descriptor     Descriptor
	schema         config.SchemaView
	aliases        []string
	provenance     Provenance
	problems       []diagnostic.Item
	implementation *componentImplementation
	execution      drive.Binding
	executionSet   bool
	traits         traitStore
}

type componentOptions struct {
	aliases        []string
	provenance     Provenance
	implementation *componentImplementation
	problems       []diagnostic.Item
	execution      drive.Binding
	executionSet   bool
	traits         traitStore
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
// registration or initialization side effects. Descriptor fields left unset
// are inherited from the parent plugin by Define; a standalone component view
// displays its marker name when DisplayName is unset.
func NewComponent[Marker any, C any](descriptor Descriptor, schema config.Schema[C], options ...ComponentOption) Component {
	identity, identityErr := identityOf[Marker]()
	componentOptionsValue := componentOptions{provenance: descriptor.Provenance}
	for _, option := range options {
		if option != nil {
			option(&componentOptionsValue)
		}
	}
	result := Component{
		identity:       identity,
		descriptor:     descriptor,
		schema:         schema.View(),
		aliases:        append([]string(nil), componentOptionsValue.aliases...),
		provenance:     componentOptionsValue.provenance,
		implementation: componentOptionsValue.implementation,
		problems:       cloneItems(componentOptionsValue.problems),
		execution:      componentOptionsValue.execution,
		executionSet:   componentOptionsValue.executionSet,
		traits:         cloneTraits(componentOptionsValue.traits),
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
	if result.executionSet && result.implementation != nil && len(result.implementation.problems) == 0 {
		if err := result.execution.Validate(result.implementation.ports); err != nil {
			result.problems = append(result.problems, diagnostic.NewItem("plugin.execution-ports", diagnostic.ErrorSeverity, diagnostic.Path{Component: result.identity.String()}, "component execution binding does not match its Ports", map[string]string{"cause": err.Error()}))
		}
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

// Resolve applies a control-plane patch through the component's captured
// schema resolver.
func (c Component) Resolve(patch config.Patch) (config.ResolvedView, error) {
	return c.schema.Resolve(patch)
}

// Aliases returns a copy of component aliases.
func (c Component) Aliases() []string { return append([]string(nil), c.aliases...) }

// Provenance returns build provenance.
func (c Component) Provenance() Provenance { return c.provenance }

// Traits returns inert, sorted trait descriptions. Live values remain
// accessible only through the trait-owning package's typed accessor.
func (c Component) Traits() []TraitDescriptor { return traitDescriptors(c.traits) }

// Ports returns the immutable static port shape declared by the component.
func (c Component) Ports() flow.Shape {
	if c.implementation == nil {
		return flow.Shape{}
	}
	return c.implementation.ports.Clone()
}

// Diagnostics returns definition-time diagnostics without exposing mutable
// storage. Schema and descriptor validation is performed here so a zero or
// adapter-created component is still rejected by Host construction.
func (c Component) Diagnostics() []diagnostic.Item {
	items := cloneItems(c.problems)
	componentPath := diagnostic.Path{}
	if !c.identity.IsZero() {
		componentPath.Component = c.identity.String()
	}
	if !c.plugin.IsZero() {
		items = append(items, c.descriptor.diagnostics(componentPath)...)
	}
	if !c.schema.Valid() {
		items = append(items, c.schema.Diagnostics()...)
	}
	if c.implementation == nil && len(c.traits) == 0 {
		items = append(items, diagnostic.NewItem("plugin.spec", diagnostic.ErrorSeverity, componentPath, "component requires a typed Spec with ports, or at least one trait", nil))
	} else if c.implementation != nil {
		items = append(items, c.implementation.problems...)
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
	descriptor := c.descriptor
	if descriptor.DisplayName == "" && !c.identity.IsZero() {
		descriptor.DisplayName = c.identity.Name()
	}
	return ComponentView{
		Identity:        c.identity,
		Plugin:          c.plugin,
		Descriptor:      descriptor,
		Schema:          c.schema.Description(),
		Aliases:         c.Aliases(),
		Provenance:      c.provenance,
		Ports:           c.Ports(),
		HasSpec:         c.implementation != nil,
		HasSuggest:      c.implementation != nil && c.implementation.suggest != nil,
		SuggestionLimit: c.suggestionLimit(),
		Finalizes:       c.implementation != nil && c.implementation.finalizes,
		Executable:      c.executionSet && c.execution.Valid(),
		Contract:        c.Contract(),
		Traits:          c.Traits(),
	}
}

// Contract returns the immutable implementation contract captured by Spec.
func (c Component) Contract() Contract {
	if c.implementation == nil {
		return Contract{}
	}
	return c.implementation.contract.clone()
}

func (c Component) traitSlots() traitStore { return c.traits }

func (c Component) withPlugin(identity Identity) Component {
	c.plugin = identity
	c.aliases = append([]string(nil), c.aliases...)
	c.problems = cloneItems(c.problems)
	c.traits = cloneTraits(c.traits)
	return c
}

func (c Component) clone() Component { return c.withPlugin(c.plugin) }

// ComponentView is an immutable, surface-facing component description.
type ComponentView struct {
	Identity        Identity
	Plugin          Identity
	Descriptor      Descriptor
	Schema          config.SchemaDescription
	Aliases         []string
	Provenance      Provenance
	Ports           flow.Shape
	HasSpec         bool
	HasSuggest      bool
	SuggestionLimit int
	Finalizes       bool
	Executable      bool
	Contract        Contract
	Traits          []TraitDescriptor
}

func (c Component) suggestionLimit() int {
	if c.implementation == nil {
		return 0
	}
	return c.implementation.suggestionLimit
}

// Definition is a plugin composition returned by a plugin function such as
// Plugin() or Codec().
type Definition struct {
	identity     Identity
	descriptor   Descriptor
	components   []Component
	declarations []Declaration
	problems     []diagnostic.Item
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
		component = bindComponent(component, identity, descriptor)
		result.components[index] = component
		if _, exists := seen[component.identity]; exists {
			result.problems = append(result.problems, diagnostic.NewItem("plugin.duplicate-component", diagnostic.ErrorSeverity, diagnostic.Path{Component: component.identity.String()}, "component identity is repeated in a plugin definition", nil))
		}
		seen[component.identity] = struct{}{}
	}
	return result
}

// WithDeclarations returns a definition that owns values. Invalid and
// duplicate declarations remain attached so Host construction can report the
// complete composition error instead of silently dropping them.
func (d Definition) WithDeclarations(values ...Declaration) Definition {
	result := d.clone()
	seen := make(map[DeclarationKey]Declaration, len(result.declarations)+len(values))
	for _, declaration := range result.declarations {
		if declaration.Key().Valid() {
			seen[declaration.Key()] = declaration
		}
	}
	for _, carrier := range values {
		for _, declaration := range carrier.expand(result.identity) {
			result.declarations = append(result.declarations, declaration)
			if problem := declaration.Problem(); problem != nil {
				result.problems = append(result.problems, diagnostic.NewItem(
					"plugin.declaration",
					diagnostic.ErrorSeverity,
					diagnostic.Path{Descriptor: declaration.Key().String()},
					"owned composition declaration is invalid",
					map[string]string{"cause": problem.Error()},
				))
			}
			if !declaration.Key().Valid() {
				continue
			}
			if previous, exists := seen[declaration.Key()]; exists && !previous.SameTargets(declaration) {
				result.problems = append(result.problems, diagnostic.NewItem(
					"plugin.declaration-duplicate",
					diagnostic.ErrorSeverity,
					diagnostic.Path{Descriptor: declaration.Key().String()},
					"definition owns conflicting declarations for one key",
					nil,
				))
			}
			seen[declaration.Key()] = declaration
		}
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

// Declarations returns copied composition declarations owned by this
// definition.
func (d Definition) Declarations() []Declaration {
	return cloneDeclarations(d.declarations)
}

// Diagnostics returns definition-time and current component diagnostics.
func (d Definition) Diagnostics() []diagnostic.Item {
	items := cloneItems(d.problems)
	pluginPath := diagnostic.Path{}
	if !d.identity.IsZero() {
		pluginPath.Component = d.identity.String()
	}
	if len(d.components) == 0 {
		items = append(items, diagnostic.NewItem("plugin.empty-definition", diagnostic.ErrorSeverity, pluginPath, "plugin definition must contain at least one component", nil))
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
	d.declarations = d.Declarations()
	d.problems = cloneItems(d.problems)
	return d
}

func (d Definition) replaceComponent(target Identity, replacement Component) (Definition, bool) {
	result := d.clone()
	for index, component := range result.components {
		if component.identity == target {
			result.components[index] = bindComponent(replacement, result.identity, result.descriptor)
			return result, true
		}
	}
	return d, false
}

func bindComponent(component Component, pluginIdentity Identity, descriptor Descriptor) Component {
	component = component.clone()
	component.plugin = pluginIdentity
	component.descriptor = component.descriptor.inherit(descriptor)
	if component.descriptor.DisplayName == "" && !component.identity.IsZero() {
		component.descriptor.DisplayName = component.identity.Name()
	}
	if component.provenance == (Provenance{}) {
		component.provenance = component.descriptor.Provenance
	}
	return component
}

// DefinitionView is a read-only plugin description.
type DefinitionView struct {
	Identity     Identity
	Descriptor   Descriptor
	Components   []ComponentView
	Declarations []Declaration
}

// View returns a read-only plugin description.
func (d Definition) View() DefinitionView {
	components := d.Components()
	views := make([]ComponentView, len(components))
	for index, component := range components {
		views[index] = component.View()
	}
	return DefinitionView{Identity: d.identity, Descriptor: d.descriptor, Components: views, Declarations: d.Declarations()}
}

func cloneItems(items []diagnostic.Item) []diagnostic.Item {
	result := make([]diagnostic.Item, len(items))
	for index, item := range items {
		result[index] = item.WithPath(item.Path)
	}
	return result
}
