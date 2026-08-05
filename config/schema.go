package config

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/marker"
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
	codeSecretRedacted     = "config.secret-redacted"
)

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

// Struct starts a schema builder with a fresh default factory. Every
// top-level field in C must be registered with AddField.
//
// Marker is an empty type whose package path and name become the schema
// identity, the same rule plugins, components, schemas, properties, and
// metadata keys follow. It is separate from C so renaming the config struct
// does not change the identity a surface or fingerprint already recorded, and
// it saves third parties from inventing a collision-free string.
func Struct[Marker, C any](defaults func() C) *Builder[C] {
	canonical, err := marker.Canonical[Marker]()
	builder := &Builder[C]{defaults: defaults, identity: canonical}
	if err != nil {
		builder.problems = append(builder.problems, diagnostic.NewItem(codeMissingIdentity, diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "identity"}, "config schema "+err.Error(), nil))
	}
	return builder
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

// Preset adds a named default patch. Provenance marks SourcePreset only when a
// field's canonical value differs from the default; assigning the same value
// remains SourceDefault.
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
			items = append(items, diagnostic.NewItem(codeUnknownPreset, diagnostic.ErrorSeverity, presetPath(patch.preset), "named preset is not registered", map[string]string{"preset": patch.preset}).
				WithSuggestions(diagnostic.Suggest(patch.preset, s.presetIDs())))
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
			items = append(items, diagnostic.NewItem(codeUnknownField, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldID), "field is not registered by this schema", nil).
				WithSuggestions(diagnostic.Suggest(fieldID, s.fieldNames())))
			continue
		}
		entry := patch.fields[fieldID]
		var decoded any
		if entry.isText {
			value, err := field.decode(entry.text)
			if err != nil {
				path := diagnostic.FieldPath(fieldID)
				path.Fields = append(path.Fields, decodePath(err)...)
				code := decodeDiagnosticCode(err)
				message := "field input could not be decoded"
				if code == codeSecretRedacted {
					message = "secret field input cannot use the redaction marker"
				}
				items = append(items, diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, inputDetail(field)))
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

// fieldNames returns every name a caller could legitimately have typed for a
// field: its ID and its search aliases.
func (s Schema[C]) fieldNames() []string {
	names := make([]string, 0, len(s.fields))
	for _, field := range s.fields {
		names = append(names, field.id)
		names = append(names, field.aliases...)
	}
	return names
}

func (s Schema[C]) presetIDs() []string {
	ids := make([]string, 0, len(s.presets))
	for _, preset := range s.presets {
		ids = append(ids, preset.id)
	}
	return ids
}

func presetPath(id string) diagnostic.Path {
	return diagnostic.Path{Fields: []string{"preset", id}}
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

func (s *Schema[C]) sortDefinitions() {
	sort.Slice(s.fields, func(left, right int) bool { return s.fields[left].id < s.fields[right].id })
	sort.Slice(s.presets, func(left, right int) bool { return s.presets[left].id < s.presets[right].id })
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
