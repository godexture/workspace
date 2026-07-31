package cliflag

import (
	"fmt"
	"reflect"
	"strconv"
)

func DecodeStruct(target any, values map[string]string) error {
	return decodeStruct(target, values, true)
}

// ApplyStruct decodes values onto target without running whole-configuration
// validation. It is used by the configuration resolver, which applies
// dynamic defaults and normalization before validating the completed value.
func ApplyStruct(target any, values map[string]string) error {
	return decodeStruct(target, values, false)
}

// ValidateStruct runs the target configuration's Validate method, if any.
func ValidateStruct(target any) error {
	value, _, err := structValue(target)
	if err != nil {
		return err
	}
	return validate(value)
}

func decodeStruct(target any, values map[string]string, validateResult bool) error {
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
		if err := applyField(copy, field, []string{raw}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if validateResult {
		if err := validate(copy); err != nil {
			return err
		}
	}
	value.Set(copy)
	return nil
}
