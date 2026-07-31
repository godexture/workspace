// Package config resolves sparse plugin configuration values into validated,
// effective configurations and dynamic field state.
package config

import (
	"fmt"
	"math"
	"slices"

	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/cliflag"
)

type Mode uint8

const (
	Strict Mode = iota
	Draft
)

type Fields map[string]struct{}

func (f Fields) Has(name string) bool {
	_, ok := f[name]
	return ok
}

type Context struct {
	Mode     Mode
	Explicit Fields
}

type Range struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

type Slot struct {
	Index   int     `json:"index"`
	Label   string  `json:"label"`
	Default float64 `json:"default"`
}

type Field struct {
	Name      string   `json:"name"`
	Active    bool     `json:"active"`
	Unit      string   `json:"unit,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Range     *Range   `json:"range,omitempty"`
	Slots     []Slot   `json:"slots,omitempty"`
}

type Source string

const (
	SourceDefault    Source = "default"
	SourceDynamic    Source = "dynamic"
	SourceExplicit   Source = "explicit"
	SourceNormalized Source = "normalized"
)

type Resolution struct {
	Values  map[string]string `json:"values"`
	Sources map[string]Source `json:"sources"`
	Updates map[string]string `json:"updates,omitempty"`
	Fields  []Field           `json:"fields"`
}

type Dynamic interface {
	ResolveConfiguration(Context) ([]Field, error)
}

func Resolve(value registry.Manifest, values map[string]string, mode Mode) (registry.Configuration, Resolution, error) {
	configuration := value.Default()
	if err := cliflag.ApplyStruct(configuration, values); err != nil {
		return nil, Resolution{}, err
	}
	before, err := rawValues(configuration)
	if err != nil {
		return nil, Resolution{}, err
	}

	explicit := make(Fields, len(values))
	for name := range values {
		explicit[name] = struct{}{}
	}
	fields := []Field{}
	if dynamic, ok := configuration.(Dynamic); ok {
		fields, err = dynamic.ResolveConfiguration(Context{Mode: mode, Explicit: explicit})
		if err != nil {
			return nil, Resolution{}, err
		}
	}
	if err := validateFields(fields, before); err != nil {
		return nil, Resolution{}, err
	}
	if err := cliflag.ValidateStruct(configuration); err != nil {
		return nil, Resolution{}, err
	}
	after, err := rawValues(configuration)
	if err != nil {
		return nil, Resolution{}, err
	}

	sources := make(map[string]Source, len(after))
	updates := make(map[string]string)
	dynamic := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.Active {
			dynamic[field.Name] = struct{}{}
		}
	}
	for name, resolved := range after {
		_, isExplicit := explicit[name]
		_, isDynamic := dynamic[name]
		changed := before[name] != resolved
		switch {
		case isExplicit && changed:
			sources[name] = SourceNormalized
			updates[name] = resolved
		case isExplicit:
			sources[name] = SourceExplicit
		case changed || isDynamic:
			sources[name] = SourceDynamic
		default:
			sources[name] = SourceDefault
		}
	}
	if len(updates) == 0 {
		updates = nil
	}
	return configuration, Resolution{Values: after, Sources: sources, Updates: updates, Fields: fields}, nil
}

func rawValues(configuration registry.Configuration) (map[string]string, error) {
	items, err := cliflag.RawStructValues(configuration)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(items))
	for _, item := range items {
		values[item.Name] = item.Value
	}
	return values, nil
}

func validateFields(fields []Field, values map[string]string) error {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Name == "" {
			return fmt.Errorf("resolved configuration field name is required")
		}
		if _, ok := values[field.Name]; !ok {
			return fmt.Errorf("resolved configuration field %q does not exist", field.Name)
		}
		if slices.Contains(names, field.Name) {
			return fmt.Errorf("duplicate resolved configuration field %q", field.Name)
		}
		names = append(names, field.Name)
		for _, dependency := range field.DependsOn {
			if _, ok := values[dependency]; !ok {
				return fmt.Errorf("resolved configuration field %q depends on unknown field %q", field.Name, dependency)
			}
		}
		if field.Range != nil {
			if !finite(field.Range.Min) || !finite(field.Range.Max) || !finite(field.Range.Step) ||
				!(field.Range.Max > field.Range.Min) || !(field.Range.Step > 0) {
				return fmt.Errorf("resolved configuration field %q has invalid range", field.Name)
			}
		}
		indexes := make([]int, 0, len(field.Slots))
		for _, slot := range field.Slots {
			if slot.Index < 0 || slices.Contains(indexes, slot.Index) {
				return fmt.Errorf("resolved configuration field %q has invalid slot index %d", field.Name, slot.Index)
			}
			if slot.Label == "" {
				return fmt.Errorf("resolved configuration field %q has empty slot label", field.Name)
			}
			if !finite(slot.Default) ||
				field.Range != nil && (slot.Default < field.Range.Min || slot.Default > field.Range.Max) {
				return fmt.Errorf("resolved configuration field %q has invalid slot default", field.Name)
			}
			indexes = append(indexes, slot.Index)
		}
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
