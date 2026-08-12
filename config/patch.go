package config

import "sort"

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

// Clone returns an independent sparse patch with the same entries.
func (p Patch) Clone() Patch { return p.clone() }

func (p Patch) clone() Patch {
	result := Patch{preset: p.preset, fields: make(map[string]patchValue, len(p.fields))}
	for field, value := range p.fields {
		result.fields[field] = value
	}
	return result
}
