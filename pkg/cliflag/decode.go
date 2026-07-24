package cliflag

import (
	"fmt"
	"reflect"
	"strconv"
)

func DecodeStruct(target any, values map[string]string) error {
	value, typeOf, err := structValue(target)
	if err != nil {
		return err
	}
	fields, err := fieldsFor(typeOf)
	if err != nil {
		return err
	}
	byName := make(map[string]field, len(fields))
	for _, field := range fields {
		byName[field.name] = field
	}
	copy := reflect.New(typeOf).Elem()
	copy.Set(value)
	if preset, exists := values["preset"]; exists {
		applier, ok := copy.Addr().Interface().(presetApplier)
		if ok {
			level, err := strconv.Atoi(preset)
			if err != nil {
				return fmt.Errorf("preset: %w", err)
			}
			applier.ApplyPreset(level)
		}
	}
	for name, raw := range values {
		if name == "preset" {
			if _, ok := copy.Addr().Interface().(presetApplier); ok {
				continue
			}
		}
		field, ok := byName[name]
		if !ok {
			return fmt.Errorf("unknown configuration field %q", name)
		}
		if err := setField(copy.Field(field.index), []string{raw}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := validateChoice(copy, field); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := validate(copy); err != nil {
		return err
	}
	value.Set(copy)
	return nil
}
