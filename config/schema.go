package config

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

const (
	codeInvalidSchema      = "config.invalid-schema"
	codeUnknownField       = "config.unknown-field"
	codeUnknownPreset      = "config.unknown-preset"
	codeInvalidInput       = "config.invalid-input"
	codeTypeMismatch       = "config.type-mismatch"
	codeCanonicalization   = "config.canonicalization"
	codeDefaultFactory     = "config.default-factory"
	codePresetFactory      = "config.preset-factory"
	codeValidation         = "config.validation"
	codeDuplicateField     = "config.duplicate-field"
	codeDuplicatePreset    = "config.duplicate-preset"
	codeFieldDependency    = "config.field-dependency"
	codeFieldPathCollision = "config.field-path-collision"
	codeMissingCodec       = "config.missing-codec"
	codeMissingClone       = "config.missing-clone"
	codeMissingAccessor    = "config.missing-accessor"
	codeMissingIdentity    = "config.missing-identity"
	codeMissingVersion     = "config.missing-version"
	codeUnregisteredField  = "config.unregistered-field"
	codeInvalidAlias       = "config.invalid-alias"
)

// Source records which stage supplied the final field value.
type Source uint8

const (
	SourceDefault Source = iota
	SourcePreset
	SourceExplicit
	SourceNormalized
)

func (s Source) String() string {
	switch s {
	case SourceDefault:
		return "default"
	case SourcePreset:
		return "preset"
	case SourceExplicit:
		return "explicit"
	case SourceNormalized:
		return "normalized"
	default:
		return "unknown"
	}
}

// ProvenanceEntry is one immutable field provenance entry.
type ProvenanceEntry struct {
	Field  string
	Source Source
}

// Provenance identifies the source of every registered field.
type Provenance struct {
	sources map[string]Source
}

// Source returns the source associated with field.
func (p Provenance) Source(field string) (Source, bool) {
	source, ok := p.sources[field]
	return source, ok
}

// Entries returns sorted field provenance without exposing internal state.
func (p Provenance) Entries() []ProvenanceEntry {
	entries := make([]ProvenanceEntry, 0, len(p.sources))
	for field, source := range p.sources {
		entries = append(entries, ProvenanceEntry{Field: field, Source: source})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Field < entries[right].Field })
	return entries
}

func cloneProvenance(provenance Provenance) Provenance {
	result := Provenance{sources: make(map[string]Source, len(provenance.sources))}
	for field, source := range provenance.sources {
		result.sources[field] = source
	}
	return result
}

// Fingerprint is a domain-separated SHA-256 digest of a canonical resolved
// config. SHA-256 is used for its stable, widely implemented cross-process
// representation; this is an identity hash, not a password hash.
type Fingerprint [32]byte

// IsZero reports whether f has not been computed.
func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }

// String returns the lowercase hexadecimal fingerprint.
func (f Fingerprint) String() string {
	return fmt.Sprintf("%x", f[:])
}

// Bytes returns a copy of the digest bytes.
func (f Fingerprint) Bytes() []byte {
	result := make([]byte, len(f))
	copy(result, f[:])
	return result
}

// Resolved is the immutable control-plane result of schema resolution. The
// Value is a defensive snapshot; callers must treat it as read-only after
// resolution.
type Resolved[C any] struct {
	Value       C
	Provenance  Provenance
	Diagnostics []diagnostic.Item
	Fingerprint Fingerprint
}

// String intentionally reports only identity metadata and never renders the
// value, which keeps SecretValue fields out of logs by default.
func (r Resolved[C]) String() string {
	return "resolved config " + r.Fingerprint.String()
}

// Patch is a sparse config update. It distinguishes omitted fields from an
// explicit zero value and can hold either a typed value or a surface string.
type Patch struct {
	preset string
	fields map[string]patchValue
}

type patchValue struct {
	text   string
	isText bool
	value  any
}

// NewPatch returns an empty sparse patch.
func NewPatch() Patch { return Patch{fields: make(map[string]patchValue)} }

// Preset selects one named preset. It returns a new patch.
func (p Patch) Preset(name string) Patch {
	result := p.clone()
	result.preset = name
	return result
}

// Set adds a typed explicit value and returns a new patch. Type validation is
// performed by the target schema so unknown fields can be reported together.
func (p Patch) Set(field string, value any) Patch {
	result := p.clone()
	result.fields[field] = patchValue{value: value}
	return result
}

// SetText adds a sparse surface value and returns a new patch. The schema's
// field codec performs decoding and validation.
func (p Patch) SetText(field, value string) Patch {
	result := p.clone()
	result.fields[field] = patchValue{text: value, isText: true}
	return result
}

// PresetName returns the selected preset name.
func (p Patch) PresetName() string { return p.preset }

// FieldIDs returns sorted IDs present in the patch without exposing values.
func (p Patch) FieldIDs() []string {
	ids := make([]string, 0, len(p.fields))
	for field := range p.fields {
		ids = append(ids, field)
	}
	sort.Strings(ids)
	return ids
}

func (p Patch) clone() Patch {
	result := Patch{preset: p.preset, fields: make(map[string]patchValue, len(p.fields))}
	for field, value := range p.fields {
		result.fields[field] = value
	}
	return result
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

// SchemaView is the type-erased control-plane view stored by catalog. It has
// no data-plane operations and exposes only immutable descriptions/errors.
type SchemaView interface {
	Valid() bool
	Diagnostics() []diagnostic.Item
	Description() SchemaDescription
}

type schemaView struct {
	valid       bool
	diagnostics []diagnostic.Item
	description SchemaDescription
}

func (v schemaView) Valid() bool { return v.valid }

func (v schemaView) Diagnostics() []diagnostic.Item {
	items := make([]diagnostic.Item, len(v.diagnostics))
	copy(items, v.diagnostics)
	return items
}

func (v schemaView) Description() SchemaDescription { return cloneSchemaDescription(v.description) }

// FieldOption changes a field's read-only description and dependency metadata.
type FieldOption func(*fieldOptions)

type fieldOptions struct {
	description Description
	aliases     []string
	dependsOn   []string
}

// Help sets a field help string.
func Help(value string) FieldOption {
	return func(options *fieldOptions) { options.description.Help = value }
}

// Unit sets a field display unit.
func Unit(value string) FieldOption {
	return func(options *fieldOptions) { options.description.Unit = value }
}

// Alias adds surface aliases. Aliases are searchable names, not identities.
func Alias(values ...string) FieldOption {
	return func(options *fieldOptions) { options.aliases = append(options.aliases, values...) }
}

// DependsOn declares control-plane field dependencies for surface projection.
func DependsOn(values ...string) FieldOption {
	return func(options *fieldOptions) { options.dependsOn = append(options.dependsOn, values...) }
}

// FieldSpec is a typed field registration produced by Field and consumed by
// Builder.AddField. The generic accessor makes Go detect field type changes.
type FieldSpec[C any] struct {
	id           string
	valueType    reflect.Type
	description  Description
	aliases      []string
	dependsOn    []string
	read         func(*C) (any, error)
	write        func(*C, any) error
	target       func(*C) uintptr
	encode       func(any) string
	decode       func(string) (any, error)
	canonical    func(any) ([]byte, error)
	normalize    func(any) (any, []diagnostic.Item)
	validate     func(any) []diagnostic.Item
	construction []diagnostic.Item
}

// Field registers a typed accessor and codec. It never panics for an invalid
// codec or accessor; the builder stores the problem for aggregate reporting.
func Field[C any, T any](id string, accessor func(*C) *T, codec Codec[T], options ...FieldOption) FieldSpec[C] {
	fieldOptionsValue := fieldOptions{description: codec.descriptionValue()}
	for _, option := range options {
		if option != nil {
			option(&fieldOptionsValue)
		}
	}
	result := FieldSpec[C]{
		id:          id,
		valueType:   reflect.TypeFor[T](),
		description: fieldOptionsValue.description,
		aliases:     append([]string(nil), fieldOptionsValue.aliases...),
		dependsOn:   append([]string(nil), fieldOptionsValue.dependsOn...),
		canonical: func(value any) ([]byte, error) {
			typed, ok := value.(T)
			if !ok {
				return nil, fmt.Errorf("field value has type %T, want %s", value, reflect.TypeFor[T]())
			}
			return codec.Canonical(typed)
		},
		encode: func(value any) string {
			typed, ok := value.(T)
			if !ok {
				return "<invalid>"
			}
			return codec.Encode(typed)
		},
		decode: func(value string) (any, error) {
			decoded, err := codec.Decode(value)
			if err != nil {
				return nil, err
			}
			return codec.Clone(decoded), nil
		},
		normalize: func(value any) (any, []diagnostic.Item) {
			typed, ok := value.(T)
			if !ok {
				return value, []diagnostic.Item{diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "field value has the wrong type", nil)}
			}
			normalized, items := codec.normalizeValue(typed)
			return codec.Clone(normalized), items
		},
		validate: func(value any) []diagnostic.Item {
			typed, ok := value.(T)
			if !ok {
				return []diagnostic.Item{diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.Path{}, "field value has the wrong type", nil)}
			}
			return codec.validateValue(typed)
		},
		construction: codec.constructionItems(),
	}
	if accessor == nil {
		result.construction = append(result.construction, diagnostic.NewItem(codeMissingAccessor, diagnostic.ErrorSeverity, diagnostic.Path{}, "field accessor is required", nil))
	}
	if !codec.Valid() {
		result.construction = append(result.construction, diagnostic.NewItem(codeMissingCodec, diagnostic.ErrorSeverity, diagnostic.Path{}, "field codec must provide decode, encode, canonical, clone, normalize, and validation operations", nil))
	}
	result.read = func(config *C) (any, error) {
		if accessor == nil {
			return nil, fmt.Errorf("field accessor is missing")
		}
		value := accessor(config)
		if value == nil {
			return nil, fmt.Errorf("field accessor returned nil")
		}
		return codec.Clone(*value), nil
	}
	result.write = func(config *C, value any) error {
		if accessor == nil {
			return fmt.Errorf("field accessor is missing")
		}
		typed, ok := value.(T)
		if !ok {
			return fmt.Errorf("field value has type %T, want %s", value, reflect.TypeFor[T]())
		}
		target := accessor(config)
		if target == nil {
			return fmt.Errorf("field accessor returned nil")
		}
		*target = codec.Clone(typed)
		return nil
	}
	result.target = func(config *C) uintptr {
		defer func() {
			if recover() != nil {
				// An invalid accessor is reported by its normal read/write path.
			}
		}()
		if accessor == nil || config == nil {
			return 0
		}
		value := accessor(config)
		if value == nil {
			return 0
		}
		return reflect.ValueOf(value).Pointer()
	}
	return result
}

type presetSpec[C any] struct {
	id    string
	apply func(*C)
}

// Builder accumulates a typed schema without mutating package-level state.
// Errors are retained until Build so component and host construction can
// aggregate them with identity and descriptor diagnostics.
type Builder[C any] struct {
	defaults func() C
	identity string
	version  string
	fields   []FieldSpec[C]
	presets  []presetSpec[C]
	validate func(C) []diagnostic.Item
	problems []diagnostic.Item
}

// Struct starts a schema builder with a fresh default factory. Every mutable
// top-level field in C must be registered with AddField.
func Struct[C any](defaults func() C) *Builder[C] {
	return &Builder[C]{defaults: defaults}
}

// Identity sets a stable schema identity independent from the Go config type.
func (b *Builder[C]) Identity(value string) *Builder[C] {
	if b == nil {
		return b
	}
	b.identity = value
	return b
}

// Version sets the schema decoder/fingerprint version.
func (b *Builder[C]) Version(value string) *Builder[C] {
	if b == nil {
		return b
	}
	b.version = value
	return b
}

// AddField appends a typed field registration.
func (b *Builder[C]) AddField(field FieldSpec[C]) *Builder[C] {
	if b == nil {
		return b
	}
	b.fields = append(b.fields, cloneFieldSpec(field))
	return b
}

// Preset adds a named default patch.
func (b *Builder[C]) Preset(id string, apply func(*C)) *Builder[C] {
	if b == nil {
		return b
	}
	b.presets = append(b.presets, presetSpec[C]{id: id, apply: apply})
	return b
}

// Validate adds a schema-level validator. It may return multiple structured
// diagnostics and must redact any sensitive values it describes.
func (b *Builder[C]) Validate(validate func(C) []diagnostic.Item) *Builder[C] {
	if b == nil {
		return b
	}
	b.validate = validate
	return b
}

// Build freezes the builder into a Schema. It validates defaults, presets,
// field graph metadata, and canonicalization without panicking.
func (b *Builder[C]) Build() Schema[C] {
	if b == nil {
		return Schema[C]{problems: []diagnostic.Item{diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema builder is nil", nil)}}
	}
	result := Schema[C]{
		built:    true,
		defaults: b.defaults,
		identity: b.identity,
		version:  b.version,
		fields:   make([]FieldSpec[C], len(b.fields)),
		presets:  make([]presetSpec[C], len(b.presets)),
		validate: b.validate,
		problems: append([]diagnostic.Item(nil), b.problems...),
	}
	for index, field := range b.fields {
		result.fields[index] = cloneFieldSpec(field)
	}
	for index, preset := range b.presets {
		result.presets[index] = preset
	}
	result.problems = append(result.problems, result.validateDefinition()...)
	result.sortDefinitions()
	return result
}

// Schema is a frozen typed configuration contract.
type Schema[C any] struct {
	built    bool
	defaults func() C
	identity string
	version  string
	fields   []FieldSpec[C]
	presets  []presetSpec[C]
	validate func(C) []diagnostic.Item
	problems []diagnostic.Item
}

// Valid reports whether schema construction completed without errors.
func (s Schema[C]) Valid() bool { return s.built && !hasError(s.problems) }

// Err returns all schema construction diagnostics, or nil when valid.
func (s Schema[C]) Err() error { return diagnosticError(s.schemaProblems()) }

// Diagnostics returns a copy of schema construction diagnostics.
func (s Schema[C]) Diagnostics() []diagnostic.Item { return cloneItems(s.schemaProblems()) }

// Default returns a fresh snapshot on every call, including nested mutable
// values returned by the default factory.
func (s Schema[C]) Default() C {
	value, _ := s.defaultValue()
	return value
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
	return schemaView{valid: s.Valid(), diagnostics: s.Diagnostics(), description: s.Description()}
}

// Canonical returns a deterministic encoding sorted by field identity. It is
// independent of registration order and map iteration order.
func (s Schema[C]) Canonical(value C) ([]byte, error) {
	if !s.Valid() {
		return nil, s.Err()
	}
	canonical, items := s.canonicalValue(value)
	if hasError(items) {
		return nil, diagnosticError(items)
	}
	return canonical, nil
}

// Resolve applies default, one named preset, explicit patch, normalization,
// validation, and canonicalization in that fixed order.
func (s Schema[C]) Resolve(patch Patch) (Resolved[C], error) {
	value, factoryItems := s.defaultValue()
	if len(factoryItems) != 0 {
		return Resolved[C]{Diagnostics: factoryItems}, diagnosticError(factoryItems)
	}
	provenance := Provenance{sources: make(map[string]Source, len(s.fields))}
	for _, field := range s.fields {
		provenance.sources[field.id] = SourceDefault
	}
	var items []diagnostic.Item
	if patch.preset != "" {
		preset, ok := s.preset(patch.preset)
		if !ok {
			items = append(items, diagnostic.NewItem(codeUnknownPreset, diagnostic.ErrorSeverity, diagnostic.Path{}, "named preset is not registered", map[string]string{"preset": patch.preset}))
		} else if preset.apply == nil {
			items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "preset has no apply function", nil))
		} else if err := callPreset(preset.apply, &value); err != nil {
			items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "preset application failed", nil))
		} else {
			presetValue, snapshotItems := s.snapshot(value)
			items = append(items, snapshotItems...)
			defaultValue, _ := s.defaultValue()
			for _, field := range s.fields {
				before, beforeErr := field.read(&defaultValue)
				after, afterErr := field.read(&presetValue)
				beforeCanonical, beforeCanonicalErr := field.canonical(before)
				afterCanonical, afterCanonicalErr := field.canonical(after)
				if beforeErr == nil && afterErr == nil && beforeCanonicalErr == nil && afterCanonicalErr == nil && !bytes.Equal(beforeCanonical, afterCanonical) {
					provenance.sources[field.id] = SourcePreset
				}
			}
		}
	}

	for _, fieldID := range patch.FieldIDs() {
		field, ok := s.field(fieldID)
		if !ok {
			items = append(items, diagnostic.NewItem(codeUnknownField, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldID), "field is not registered by this schema", nil))
			continue
		}
		entry := patch.fields[fieldID]
		var decoded any
		if entry.isText {
			value, err := field.decode(entry.text)
			if err != nil {
				path := diagnostic.FieldPath(fieldID)
				path.Fields = append(path.Fields, decodePath(err)...)
				items = append(items, diagnostic.NewItem(codeInvalidInput, diagnostic.ErrorSeverity, path, "field input could not be decoded", inputDetail(field)))
				continue
			}
			decoded = value
		} else {
			decoded = entry.value
		}
		if err := field.write(&value, decoded); err != nil {
			items = append(items, diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldID), "field input has the wrong type", inputDetail(field)))
			continue
		}
		provenance.sources[fieldID] = SourceExplicit
	}

	return s.finish(value, provenance, items)
}

// ResolveValue resolves a complete value. Every registered field is explicit,
// including fields whose value is the type's zero value.
func (s Schema[C]) ResolveValue(value C) (Resolved[C], error) {
	value, snapshotItems := s.snapshot(value)
	provenance := Provenance{sources: make(map[string]Source, len(s.fields))}
	for _, field := range s.fields {
		provenance.sources[field.id] = SourceExplicit
	}
	return s.finish(value, provenance, snapshotItems)
}

func (s Schema[C]) finish(value C, provenance Provenance, items []diagnostic.Item) (Resolved[C], error) {
	if !s.Valid() {
		items = append(items, s.schemaProblems()...)
		value, snapshotItems := s.snapshot(value)
		items = append(items, snapshotItems...)
		return Resolved[C]{Value: value, Provenance: cloneProvenance(provenance), Diagnostics: cloneItems(items)}, diagnosticError(items)
	}

	for _, field := range s.fields {
		current, err := field.read(&value)
		if err != nil {
			items = append(items, diagnostic.NewItem(codeValidation, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "field could not be read", nil))
			continue
		}
		before, beforeErr := field.canonical(current)
		normalized, normalizationItems := field.normalize(current)
		for _, item := range normalizationItems {
			items = append(items, prefixItem(item, field.id))
		}
		if err := field.write(&value, normalized); err != nil {
			items = append(items, diagnostic.NewItem(codeValidation, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "normalized field could not be stored", nil))
			continue
		}
		after, afterErr := field.canonical(normalized)
		if beforeErr == nil && afterErr == nil && !bytes.Equal(before, after) {
			provenance.sources[field.id] = SourceNormalized
		}
		for _, item := range field.validate(normalized) {
			items = append(items, prefixItem(item, field.id))
		}
	}
	for _, item := range s.validateSchema(value) {
		items = append(items, item)
	}

	canonical, canonicalItems := s.canonicalValue(value)
	items = append(items, canonicalItems...)
	value, snapshotItems := s.snapshot(value)
	items = append(items, snapshotItems...)
	resolved := Resolved[C]{
		Value:       value,
		Provenance:  cloneProvenance(provenance),
		Diagnostics: cloneItems(items),
	}
	if hasError(items) {
		return resolved, diagnosticError(items)
	}
	resolved.Fingerprint = hashCanonical(canonical)
	return resolved, nil
}

func (s Schema[C]) validateValue(value C) []diagnostic.Item {
	var items []diagnostic.Item
	for _, field := range s.fields {
		fieldValue, err := field.read(&value)
		if err != nil {
			items = append(items, diagnostic.NewItem(codeValidation, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "field could not be read", nil))
			continue
		}
		for _, item := range field.validate(fieldValue) {
			items = append(items, prefixItem(item, field.id))
		}
	}
	if s.validate != nil {
		items = append(items, s.validateSchema(value)...)
	}
	return items
}

func (s Schema[C]) validateSchema(value C) []diagnostic.Item {
	if s.validate == nil {
		return nil
	}
	var items []diagnostic.Item
	validated, snapshotItems := s.snapshot(value)
	items = append(items, snapshotItems...)
	for _, item := range s.validate(validated) {
		items = append(items, item)
	}
	return items
}

func (s Schema[C]) canonicalValue(value C) ([]byte, []diagnostic.Item) {
	canonical := []byte("godec/config/canonical/v1\x00")
	canonical = appendLength(canonical, []byte(s.identity))
	canonical = appendLength(canonical, []byte(s.version))
	var items []diagnostic.Item
	for _, field := range s.fields {
		fieldValue, err := field.read(&value)
		if err != nil {
			items = append(items, diagnostic.NewItem(codeCanonicalization, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "field could not be read for canonicalization", nil))
			continue
		}
		encoded, err := field.canonical(fieldValue)
		if err != nil {
			message := "field cannot be canonicalized"
			if field.description.Secret {
				message = "secret field cannot be canonicalized"
			}
			items = append(items, diagnostic.NewItem(codeCanonicalization, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), message, nil))
			continue
		}
		canonical = appendLength(canonical, []byte(field.id))
		canonical = appendLength(canonical, []byte(field.description.Type))
		canonical = appendLength(canonical, encoded)
	}
	return canonical, items
}

func (s Schema[C]) defaultValue() (C, []diagnostic.Item) {
	if s.defaults == nil {
		var zero C
		return zero, []diagnostic.Item{diagnostic.NewItem(codeDefaultFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema default factory is required", nil)}
	}
	var value C
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
				value = *new(C)
			}
		}()
		value = s.defaults()
	}()
	if panicked {
		return value, []diagnostic.Item{diagnostic.NewItem(codeDefaultFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema default factory panicked", nil)}
	}
	return s.snapshot(value)
}

func (s Schema[C]) snapshot(value C) (C, []diagnostic.Item) {
	result := value
	items := s.snapshotItemsInto(&result)
	return result, items
}

func (s Schema[C]) snapshotItemsInto(value *C) []diagnostic.Item {
	if value == nil {
		return []diagnostic.Item{diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, diagnostic.Path{}, "config snapshot target is nil", nil)}
	}
	var items []diagnostic.Item
	for _, field := range s.fields {
		fieldValue, err := field.read(value)
		if err != nil {
			items = append(items, diagnostic.NewItem(codeValidation, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "field could not be copied for snapshot", nil))
			continue
		}
		if err := field.write(value, fieldValue); err != nil {
			items = append(items, diagnostic.NewItem(codeValidation, diagnostic.ErrorSeverity, diagnostic.FieldPath(field.id), "field snapshot could not be stored", nil))
		}
	}
	return items
}

func (s Schema[C]) schemaProblems() []diagnostic.Item {
	items := cloneItems(s.problems)
	if !s.built {
		items = append(items, diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema has not been built", nil))
	}
	return items
}

func callPreset[C any](apply func(*C), value *C) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("preset panicked")
		}
	}()
	apply(value)
	return nil
}

func (s Schema[C]) field(id string) (FieldSpec[C], bool) {
	index := sort.Search(len(s.fields), func(index int) bool { return s.fields[index].id >= id })
	if index >= len(s.fields) || s.fields[index].id != id {
		return FieldSpec[C]{}, false
	}
	return s.fields[index], true
}

func (s Schema[C]) preset(id string) (presetSpec[C], bool) {
	index := sort.Search(len(s.presets), func(index int) bool { return s.presets[index].id >= id })
	if index >= len(s.presets) || s.presets[index].id != id {
		return presetSpec[C]{}, false
	}
	return s.presets[index], true
}

func (s Schema[C]) validateDefinition() []diagnostic.Item {
	var items []diagnostic.Item
	var defaultValue C
	defaultReady := false
	if strings.TrimSpace(s.identity) == "" {
		items = append(items, diagnostic.NewItem(codeMissingIdentity, diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "identity"}, "schema identity is required", nil))
	}
	if strings.TrimSpace(s.version) == "" {
		items = append(items, diagnostic.NewItem(codeMissingVersion, diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "version"}, "schema version is required", nil))
	}
	if s.defaults == nil {
		items = append(items, diagnostic.NewItem(codeDefaultFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema default factory is required", nil))
	}
	fieldIDs := make(map[string]struct{}, len(s.fields))
	for _, field := range s.fields {
		fieldIDs[field.id] = struct{}{}
	}
	aliases := make(map[string]string)
	for _, field := range s.fields {
		path := diagnostic.FieldPath(field.id)
		if field.id == "" || strings.TrimSpace(field.id) != field.id {
			items = append(items, diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, path, "field ID must be non-empty and trimmed", nil))
		}
		if countFieldIDs(s.fields, field.id) > 1 {
			items = append(items, diagnostic.NewItem(codeDuplicateField, diagnostic.ErrorSeverity, path, "field IDs must be unique", nil))
		}
		for _, problem := range field.construction {
			items = append(items, prefixItem(problem, field.id))
		}
		seenAliases := make(map[string]struct{}, len(field.aliases))
		for _, alias := range field.aliases {
			if alias == "" || strings.TrimSpace(alias) != alias || strings.IndexFunc(alias, func(r rune) bool { return r == '.' || r == '\\' || r == '/' }) >= 0 {
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field alias must be a trimmed search name without path separators", nil))
			}
			if _, exists := seenAliases[alias]; exists {
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field aliases must be unique within a field", nil))
			}
			seenAliases[alias] = struct{}{}
			if previous, exists := aliases[alias]; exists && previous != field.id {
				// Aliases may be shared across components, but not within one schema.
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field aliases must identify one field within a schema", nil))
			}
			aliases[alias] = field.id
		}
		for _, dependency := range field.dependsOn {
			if _, exists := fieldIDs[dependency]; !exists {
				items = append(items, diagnostic.NewItem(codeFieldDependency, diagnostic.ErrorSeverity, path, "field depends on an unknown field", map[string]string{"dependency": dependency}))
			}
		}
	}
	for left, leftField := range s.fields {
		for right := left + 1; right < len(s.fields); right++ {
			if pathCollision(leftField.id, s.fields[right].id) {
				items = append(items, diagnostic.NewItem(codeFieldPathCollision, diagnostic.ErrorSeverity, diagnostic.FieldPath(leftField.id), "nested field paths collide", map[string]string{"other": s.fields[right].id}))
			}
		}
	}
	if hasDependencyCycle(s.fields) {
		items = append(items, diagnostic.NewItem(codeFieldDependency, diagnostic.ErrorSeverity, diagnostic.Path{}, "field dependencies contain a cycle", nil))
	}
	if s.defaults != nil {
		value, factoryItems := s.defaultValue()
		items = append(items, factoryItems...)
		items = append(items, s.validateUnregisteredMutableFields(value)...)
		defaultValue = value
		defaultReady = len(factoryItems) == 0
	}
	presetIDs := make(map[string]struct{}, len(s.presets))
	for _, preset := range s.presets {
		path := diagnostic.Path{Fields: []string{"preset", preset.id}}
		if preset.id == "" || strings.TrimSpace(preset.id) != preset.id {
			items = append(items, diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, path, "preset ID must be non-empty and trimmed", nil))
		}
		if _, exists := presetIDs[preset.id]; exists {
			items = append(items, diagnostic.NewItem(codeDuplicatePreset, diagnostic.ErrorSeverity, path, "preset IDs must be unique", nil))
		}
		presetIDs[preset.id] = struct{}{}
		if preset.apply == nil {
			items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, path, "preset apply function is required", nil))
		}
	}
	if defaultReady {
		value := defaultValue
		items = append(items, s.validateValue(value)...)
		_, canonicalItems := s.canonicalValue(value)
		items = append(items, canonicalItems...)
		for _, preset := range s.presets {
			if preset.apply == nil {
				continue
			}
			presetValue, snapshotItems := s.snapshot(value)
			items = append(items, snapshotItems...)
			if err := callPreset(preset.apply, &presetValue); err != nil {
				items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, diagnostic.Path{Fields: []string{"preset", preset.id}}, "preset application failed", nil))
				continue
			}
			for _, item := range s.validateValue(presetValue) {
				items = append(items, prefixPresetItem(item, preset.id))
			}
			_, presetCanonicalItems := s.canonicalValue(presetValue)
			for _, item := range presetCanonicalItems {
				items = append(items, prefixPresetItem(item, preset.id))
			}
		}
	}
	return items
}

// Mutable means a type containing a slice, map, pointer, interface, function,
// channel, unsafe pointer, or a mutable descendant; each top-level field must
// then be registered so its codec defines the snapshot boundary.
func (s Schema[C]) validateUnregisteredMutableFields(value C) []diagnostic.Item {
	typ := reflect.TypeFor[C]()
	if typ.Kind() != reflect.Struct {
		if mutableType(typ) && len(s.fields) == 0 {
			return []diagnostic.Item{diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.FieldPath("<root>"), "mutable config value must be registered through a field codec", map[string]string{"type": typ.String()})}
		}
		return nil
	}

	root := reflect.ValueOf(&value).Elem()
	registered := make(map[uintptr]string, len(s.fields))
	for _, field := range s.fields {
		if field.target == nil {
			continue
		}
		if pointer := field.target(&value); pointer != 0 {
			registered[pointer] = field.id
		}
	}
	var items []diagnostic.Item
	for index := 0; index < typ.NumField(); index++ {
		fieldType := typ.Field(index)
		if !mutableType(fieldType.Type) {
			continue
		}
		fieldValue := root.Field(index)
		pointer, ok := fieldAddress(fieldValue)
		if !ok {
			continue
		}
		if _, exists := registered[pointer]; exists {
			continue
		}
		items = append(items, diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldType.Name), "mutable config field is not registered by the schema", map[string]string{"type": fieldType.Type.String()}))
	}
	return items
}

func fieldAddress(value reflect.Value) (pointer uintptr, ok bool) {
	if !value.IsValid() || !value.CanAddr() {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			pointer = 0
			ok = false
		}
	}()
	return value.Addr().Pointer(), true
}

func (s *Schema[C]) sortDefinitions() {
	sort.Slice(s.fields, func(left, right int) bool { return s.fields[left].id < s.fields[right].id })
	sort.Slice(s.presets, func(left, right int) bool { return s.presets[left].id < s.presets[right].id })
}

func cloneFieldSpec[C any](field FieldSpec[C]) FieldSpec[C] {
	field.aliases = append([]string(nil), field.aliases...)
	field.dependsOn = append([]string(nil), field.dependsOn...)
	field.description = cloneDescription(field.description)
	field.construction = cloneItems(field.construction)
	return field
}

func (field FieldSpec[C]) descriptionValue() FieldDescription {
	description := cloneDescription(field.description)
	return FieldDescription{
		ID:        field.id,
		Type:      description.Type,
		Help:      description.Help,
		Unit:      description.Unit,
		Secret:    description.Secret,
		Optional:  description.Optional,
		Auto:      description.Auto,
		Ordered:   description.Ordered,
		Range:     description.Range,
		Choices:   description.Choices,
		Aliases:   append([]string(nil), field.aliases...),
		DependsOn: append([]string(nil), field.dependsOn...),
	}
}

func inputDetail[C any](field FieldSpec[C]) map[string]string {
	detail := map[string]string{
		"source":   "patch",
		"expected": field.description.Type,
	}
	if field.description.Range != nil {
		detail["constraint"] = "range"
	}
	return detail
}

func prefixItem(item diagnostic.Item, field string) diagnostic.Item {
	if item.Path.IsZero() {
		item.Path = diagnostic.FieldPath(field)
	} else {
		item.Path = item.Path.Prefix(diagnostic.FieldPath(field))
	}
	return item
}

func prefixPresetItem(item diagnostic.Item, preset string) diagnostic.Item {
	item.Path = item.Path.Prefix(diagnostic.Path{Fields: []string{"preset", preset}})
	return item
}

func pathCollision(left, right string) bool {
	return left != right && (strings.HasPrefix(left, right+".") || strings.HasPrefix(right, left+"."))
}

func hasDependencyCycle[C any](fields []FieldSpec[C]) bool {
	dependencies := make(map[string][]string, len(fields))
	for _, field := range fields {
		dependencies[field.id] = append([]string(nil), field.dependsOn...)
	}
	state := make(map[string]uint8, len(fields))
	var visit func(string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		for _, dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range dependencies {
		if visit(id) {
			return true
		}
	}
	return false
}

func hashCanonical(canonical []byte) Fingerprint {
	hash := sha256.New()
	hash.Write([]byte("godec/config/fingerprint/v1\x00"))
	hash.Write(canonical)
	var result Fingerprint
	copy(result[:], hash.Sum(nil))
	return result
}

func countFieldIDs[C any](fields []FieldSpec[C], id string) int {
	count := 0
	for _, field := range fields {
		if field.id == id {
			count++
		}
	}
	return count
}

func diagnosticError(items []diagnostic.Item) error {
	if !hasError(items) {
		return nil
	}
	return diagnostic.NewError(items...)
}

func hasError(items []diagnostic.Item) bool {
	for _, item := range items {
		if item.Severity == diagnostic.ErrorSeverity {
			return true
		}
	}
	return false
}

func cloneItems(items []diagnostic.Item) []diagnostic.Item {
	if len(items) == 0 {
		return nil
	}
	result := make([]diagnostic.Item, len(items))
	copy(result, items)
	for index, item := range result {
		result[index] = item.WithPath(item.Path)
	}
	return result
}
