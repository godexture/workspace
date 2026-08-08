package config

import "github.com/godexture/godec/diagnostic"

// Description is the read-only surface description of a field codec.
type Description struct {
	Type     string
	Help     string
	Unit     string
	Secret   bool
	Optional bool
	Auto     bool
	Ordered  bool
	Range    *Range
	Choices  []ChoiceDescription
}

// Range describes a numeric field constraint. Step is zero when no step was
// declared.
type Range struct {
	Min  float64
	Max  float64
	Step float64
}

// ChoiceDescription is the stable surface representation of an enum choice.
type ChoiceDescription struct {
	ID    string
	Label string
}

// FieldDescription is the immutable read-only description of one schema
// field.
type FieldDescription struct {
	ID        string
	Type      string
	Help      string
	Unit      string
	Secret    bool
	Optional  bool
	Auto      bool
	Ordered   bool
	Range     *Range
	Choices   []ChoiceDescription
	Aliases   []string
	DependsOn []string
}

// PresetDescription is the stable surface description of a named preset.
type PresetDescription struct {
	ID string
}

// SchemaDescription is a read-only description suitable for surface
// projection. Fields and presets are sorted by stable IDs.
type SchemaDescription struct {
	Identity string
	Version  string
	Fields   []FieldDescription
	Presets  []PresetDescription
}

func cloneFieldDescription(description FieldDescription) FieldDescription {
	description.Aliases = append([]string(nil), description.Aliases...)
	description.DependsOn = append([]string(nil), description.DependsOn...)
	description.Choices = append([]ChoiceDescription(nil), description.Choices...)
	if description.Range != nil {
		rangeCopy := *description.Range
		description.Range = &rangeCopy
	}
	return description
}

func cloneSchemaDescription(description SchemaDescription) SchemaDescription {
	fields := description.Fields
	description.Fields = make([]FieldDescription, len(fields))
	for index, field := range fields {
		description.Fields[index] = cloneFieldDescription(field)
	}
	description.Presets = append([]PresetDescription(nil), description.Presets...)
	return description
}

// ResolvedView is the type-erased result of resolving a component patch. Value
// contains the schema's concrete configuration behind an any boundary.
type ResolvedView struct {
	Schema      string
	Value       any
	Provenance  Provenance
	Diagnostics []diagnostic.Item
	Fingerprint Fingerprint
}

// SchemaView is the type-erased control-plane view stored by catalog. Its
// fields are private so only Schema[C].View can create a valid resolver.
type SchemaView struct {
	valid        bool
	diagnostics  []diagnostic.Item
	description  SchemaDescription
	resolve      func(Patch) (ResolvedView, error)
	resolveValue func(any) (ResolvedView, error)
}

// Valid reports whether the captured schema was built without errors.
func (v SchemaView) Valid() bool { return v.valid }

// Diagnostics returns schema construction diagnostics.
func (v SchemaView) Diagnostics() []diagnostic.Item {
	if len(v.diagnostics) != 0 {
		return cloneItems(v.diagnostics)
	}
	if v.resolve == nil && !v.valid {
		return []diagnostic.Item{diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema view has no captured schema", nil)}
	}
	return nil
}

// Description returns a copy of the captured schema description.
func (v SchemaView) Description() SchemaDescription { return cloneSchemaDescription(v.description) }

// Resolve applies a patch through the captured typed schema.
func (v SchemaView) Resolve(patch Patch) (ResolvedView, error) {
	if v.resolve == nil {
		items := v.Diagnostics()
		return ResolvedView{Schema: v.description.Identity, Diagnostics: cloneItems(items)}, diagnosticError(items)
	}
	return v.resolve(patch)
}

// ResolveValue validates and snapshots a complete type-erased value. It is
// used for typed config candidates returned by plugin Suggest.
func (v SchemaView) ResolveValue(value any) (ResolvedView, error) {
	if v.resolveValue == nil {
		items := v.Diagnostics()
		return ResolvedView{Schema: v.description.Identity, Diagnostics: cloneItems(items)}, diagnosticError(items)
	}
	return v.resolveValue(value)
}

// Description returns a frozen read-only schema description.
func (s Schema[C]) Description() SchemaDescription {
	description := SchemaDescription{Identity: s.identity, Version: s.version}
	description.Fields = make([]FieldDescription, len(s.fields))
	for index, field := range s.fields {
		description.Fields[index] = field.descriptionValue()
	}
	description.Presets = make([]PresetDescription, len(s.presets))
	for index, preset := range s.presets {
		description.Presets[index] = PresetDescription{ID: preset.id}
	}
	return cloneSchemaDescription(description)
}

// View returns the type-erased control-plane view used by plugin/catalog.
func (s Schema[C]) View() SchemaView {
	return SchemaView{
		valid:       s.Valid(),
		diagnostics: s.Diagnostics(),
		description: s.Description(),
		resolve: func(patch Patch) (ResolvedView, error) {
			resolved, err := s.Resolve(patch)
			return ResolvedView{
				Schema:      s.identity,
				Value:       resolved.Value,
				Provenance:  cloneProvenance(resolved.Provenance),
				Diagnostics: cloneItems(resolved.Diagnostics),
				Fingerprint: resolved.Fingerprint,
			}, err
		},
		resolveValue: func(value any) (ResolvedView, error) {
			typed, ok := value.(C)
			if !ok {
				item := diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "complete config value has the wrong type", nil)
				return ResolvedView{Schema: s.identity, Diagnostics: []diagnostic.Item{item}}, diagnostic.NewError(item)
			}
			resolved, err := s.ResolveValue(typed)
			return ResolvedView{
				Schema:      s.identity,
				Value:       resolved.Value,
				Provenance:  cloneProvenance(resolved.Provenance),
				Diagnostics: cloneItems(resolved.Diagnostics),
				Fingerprint: resolved.Fingerprint,
			}, err
		},
	}
}

func cloneDescription(description Description) Description {
	description.Choices = append([]ChoiceDescription(nil), description.Choices...)
	if description.Range != nil {
		rangeCopy := *description.Range
		description.Range = &rangeCopy
	}
	return description
}
