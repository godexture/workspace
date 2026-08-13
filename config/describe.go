package config

import (
	"fmt"
	"strconv"

	"github.com/godexture/godec/diagnostic"
)

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

// ResolvedView is the type-erased result of resolving a component patch. Its
// concrete configuration stays behind an accessor that hands out a fresh
// snapshot: Shape and Compile are separate phases over the same fingerprint,
// and neither may change what the other sees.
type ResolvedView struct {
	schema      string
	value       any
	clone       func(any) (any, error)
	provenance  Provenance
	diagnostics []diagnostic.Item
	fingerprint Fingerprint
	summary     Summary
}

// Schema returns the identity of the schema that produced this view.
func (v ResolvedView) Schema() string { return v.schema }

// Value returns an independent snapshot of the resolved configuration behind
// an any boundary. It is nil when resolution produced no value.
func (v ResolvedView) Value() any {
	if v.clone == nil {
		return v.value
	}
	cloned, err := v.clone(v.value)
	if err != nil {
		return nil
	}
	return cloned
}

// Provenance reports which stage supplied each registered field.
func (v ResolvedView) Provenance() Provenance { return cloneProvenance(v.provenance) }

// Diagnostics returns the resolution diagnostics.
func (v ResolvedView) Diagnostics() []diagnostic.Item { return cloneItems(v.diagnostics) }

// Fingerprint identifies the canonical resolved value.
func (v ResolvedView) Fingerprint() Fingerprint { return v.fingerprint }

// String reports only schema and fingerprint identity metadata.
func (v ResolvedView) String() string {
	return "resolved config schema=" + strconv.Quote(v.schema) + " fingerprint=" + v.fingerprint.String()
}

// Format prevents every fmt verb, including %#v, from traversing Value.
func (v ResolvedView) Format(state fmt.State, verb rune) {
	writeSafeFormat(state, verb, v.String())
}

// Summary returns an inert redacted projection for Plans and surfaces.
func (v ResolvedView) Summary() Summary { return v.summary.clone() }

// SchemaView is the type-erased control-plane view stored by catalog. Its
// fields are private so only Schema[C].View can create a valid resolver.
type SchemaView struct {
	valid        bool
	diagnostics  []diagnostic.Item
	description  SchemaDescription
	key          func(string) (Key, bool)
	resolve      func(Patch) (ResolvedView, error)
	resolveValue func(any) (ResolvedView, error)
	patch        func(ResolvedView) (Patch, error)
}

// Key returns the patch key for a registered field of the captured schema. It
// is how a caller that only has a type-erased view builds a typed patch.
func (v SchemaView) Key(id string) (Key, bool) {
	if v.key == nil {
		return Key{}, false
	}
	return v.key(id)
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
		return ResolvedView{schema: v.description.Identity, diagnostics: cloneItems(items)}, diagnosticError(items)
	}
	return v.resolve(patch)
}

// ResolveValue validates and snapshots a complete type-erased value. It is
// used for typed config candidates returned by plugin Suggest.
func (v SchemaView) ResolveValue(value any) (ResolvedView, error) {
	if v.resolveValue == nil {
		items := v.Diagnostics()
		return ResolvedView{schema: v.description.Identity, diagnostics: cloneItems(items)}, diagnosticError(items)
	}
	return v.resolveValue(value)
}

// Patch snapshots every resolved field back into a typed sparse patch. It is
// used internally when a planner inserts a suggested config into a resolved
// graph; Patch itself does not expose stored values.
func (v SchemaView) Patch(resolved ResolvedView) (Patch, error) {
	if v.patch == nil || resolved.schema != v.description.Identity || resolved.fingerprint.IsZero() {
		item := diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "resolved config does not belong to this schema", nil)
		return Patch{}, diagnostic.NewError(item)
	}
	return v.patch(resolved)
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
		key:         s.Key,
		resolve: func(patch Patch) (ResolvedView, error) {
			resolved, err := s.Resolve(patch)
			return s.resolvedView(resolved), err
		},
		resolveValue: func(value any) (ResolvedView, error) {
			typed, ok := value.(C)
			if !ok {
				item := diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "complete config value has the wrong type", nil)
				return ResolvedView{schema: s.identity, diagnostics: []diagnostic.Item{item}}, diagnostic.NewError(item)
			}
			resolved, err := s.ResolveValue(typed)
			return s.resolvedView(resolved), err
		},
		patch: func(resolved ResolvedView) (Patch, error) {
			value, ok := resolved.Value().(C)
			if !ok {
				item := diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "resolved config value has the wrong type", nil)
				return Patch{}, diagnostic.NewError(item)
			}
			return s.patch(value)
		},
	}
}

func (s Schema[C]) resolvedView(resolved Resolved[C]) ResolvedView {
	return ResolvedView{
		schema: s.identity,
		value:  resolved.value,
		clone: func(value any) (any, error) {
			typed, ok := value.(C)
			if !ok {
				return nil, diagnostic.NewError(diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "resolved config value has the wrong type", nil))
			}
			snapshot, _ := s.snapshot(typed)
			return snapshot, nil
		},
		provenance:  cloneProvenance(resolved.provenance),
		diagnostics: cloneItems(resolved.diagnostics),
		fingerprint: resolved.fingerprint,
		summary:     s.summary(resolved.value, resolved.provenance, resolved.fingerprint),
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
