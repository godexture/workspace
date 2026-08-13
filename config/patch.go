package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

// Key is a handle to one registered field of one schema. A patch that accepts
// a bare field name cannot snapshot a slice, map, or pointer, because guessing
// how to copy an arbitrary Go value is exactly what C17 forbids. A Key carries
// the field's declared clone, so only the schema that registered the field can
// authorize a typed patch entry for it.
type Key struct {
	schema string
	field  string
	typ    string
	clone  func(any) (any, error)
}

func (k Key) Valid() bool    { return k.schema != "" && k.field != "" && k.clone != nil }
func (k Key) Schema() string { return k.schema }
func (k Key) Field() string  { return k.field }
func (k Key) Type() string   { return k.typ }
func (k Key) String() string { return k.schema + "." + k.field }

// Patch is a sparse config update. It distinguishes omitted fields from an
// explicit zero value and can hold either a typed value or a surface string.
// Both forms are snapshots: a patch never shares mutable state with whoever
// built it, so a Job keeps the meaning it had when it was constructed.
type Patch struct {
	preset   string
	fields   map[string]patchValue
	problems []diagnostic.Item
}

type patchValue struct {
	// A closure keeps the payload opaque when reflection-based formatting
	// reaches Patch through an unexported field or a named type, and hands out
	// a fresh snapshot so no reader can reach the stored one either.
	payload func() (any, error)
	schema  string
	isText  bool
	source  Source
}

// NewPatch returns an empty sparse patch.
func NewPatch() Patch { return Patch{fields: make(map[string]patchValue)} }

// Preset selects one named preset. It returns a new patch.
func (p Patch) Preset(name string) Patch {
	result := p.clone()
	result.preset = name
	return result
}

// Set stores a typed explicit value for a schema field and returns a new
// patch. The value is snapshotted through the field's declared clone, so a
// later change to the caller's slice, map, or pointer cannot change what the
// patch means. A key from another schema, or a value of the wrong type, is
// retained as a diagnostic and reported when the patch is resolved.
func (p Patch) Set(key Key, value any) Patch {
	result := p.clone()
	if !key.Valid() {
		result.problems = append(result.problems, diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.Path{}, "patch key does not identify a registered schema field", nil))
		return result
	}
	stored, err := key.clone(value)
	if err != nil {
		result.problems = append(result.problems, diagnostic.NewItem(codeTypeMismatch, diagnostic.ErrorSeverity, diagnostic.FieldPath(key.field), "patch value does not match the field type", map[string]string{"expected": key.typ}))
		return result
	}
	result.fields[key.field] = patchValue{
		payload: func() (any, error) { return key.clone(stored) },
		schema:  key.schema,
		source:  SourceExplicit,
	}
	return result
}

// SetText adds a sparse surface value and returns a new patch. Text needs no
// schema: it is immutable, and the target field's codec decodes and clones it
// during resolution.
func (p Patch) SetText(field, value string) Patch {
	result := p.clone()
	result.fields[field] = patchValue{
		payload: func() (any, error) { return value, nil },
		isText:  true,
		source:  SourceExplicit,
	}
	return result
}

// Planned returns a copy whose supplied fields are attributed to the planner.
func (p Patch) Planned() Patch {
	result := p.clone()
	for field, value := range result.fields {
		value.source = SourcePlanner
		result.fields[field] = value
	}
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

// String reports only patch identity metadata. Values remain hidden because a
// Patch has not yet been matched with a schema that can identify secret fields.
func (p Patch) String() string {
	var result strings.Builder
	result.WriteString("config patch")
	if p.preset != "" {
		result.WriteString(" preset=")
		result.WriteString(strconv.Quote(p.preset))
	}
	result.WriteString(" fields=[")
	for index, field := range p.FieldIDs() {
		if index != 0 {
			result.WriteByte(' ')
		}
		result.WriteString(strconv.Quote(field))
		result.WriteByte(':')
		result.WriteString(p.fields[field].source.String())
	}
	result.WriteByte(']')
	if len(p.problems) != 0 {
		result.WriteString(" problems=")
		result.WriteString(strconv.Itoa(len(p.problems)))
	}
	return result.String()
}

// Format prevents every fmt verb, including %#v, from traversing patch values.
func (p Patch) Format(state fmt.State, verb rune) {
	writeSafeFormat(state, verb, p.String())
}

// Clone returns an independent sparse patch with the same entries.
func (p Patch) Clone() Patch { return p.clone() }

func (p Patch) clone() Patch {
	result := Patch{
		preset:   p.preset,
		fields:   make(map[string]patchValue, len(p.fields)),
		problems: cloneItems(p.problems),
	}
	for field, value := range p.fields {
		result.fields[field] = value
	}
	return result
}

func (v patchValue) value() (any, error) {
	if v.payload == nil {
		return nil, fmt.Errorf("patch entry has no value")
	}
	return v.payload()
}
