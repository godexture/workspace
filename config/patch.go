package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Patch is a sparse config update. It distinguishes omitted fields from an
// explicit zero value and can hold either a typed value or a surface string.
type Patch struct {
	preset string
	fields map[string]patchValue
}

type patchValue struct {
	// A closure keeps the payload opaque when reflection-based formatting
	// reaches Patch through an unexported field or a named type.
	payload func() any
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

// Set adds a typed explicit value and returns a new patch. Type validation is
// performed by the target schema so unknown fields can be reported together.
func (p Patch) Set(field string, value any) Patch {
	result := p.clone()
	result.fields[field] = newPatchValue(value, false, SourceExplicit)
	return result
}

// SetText adds a sparse surface value and returns a new patch. The schema's
// field codec performs decoding and validation.
func (p Patch) SetText(field, value string) Patch {
	result := p.clone()
	result.fields[field] = newPatchValue(value, true, SourceExplicit)
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
	return result.String()
}

// Format prevents every fmt verb, including %#v, from traversing patch values.
func (p Patch) Format(state fmt.State, verb rune) {
	writeSafeFormat(state, verb, p.String())
}

// Clone returns an independent sparse patch with the same entries.
func (p Patch) Clone() Patch { return p.clone() }

func (p Patch) clone() Patch {
	result := Patch{preset: p.preset, fields: make(map[string]patchValue, len(p.fields))}
	for field, value := range p.fields {
		result.fields[field] = value
	}
	return result
}

func newPatchValue(value any, isText bool, source Source) patchValue {
	return patchValue{
		payload: func() any { return value },
		isText:  isText,
		source:  source,
	}
}

func (v patchValue) value() any {
	if v.payload == nil {
		return nil
	}
	return v.payload()
}
