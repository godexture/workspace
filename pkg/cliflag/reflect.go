package cliflag

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

func fieldsFor(typeOf reflect.Type) ([]field, error) {
	fields := make([]field, 0, typeOf.NumField())
	seen := map[string]struct{}{}
	for index := range typeOf.NumField() {
		structField := typeOf.Field(index)
		tag, exists := structField.Tag.Lookup("name")
		if !exists || tag == "-" {
			continue
		}
		if !structField.IsExported() || tag == "" {
			return nil, fmt.Errorf("field %s has an invalid cli tag", structField.Name)
		}
		if !supportedType(structField.Type) {
			return nil, fmt.Errorf("field %s has unsupported CLI type %s", structField.Name, structField.Type)
		}
		if _, duplicate := seen[tag]; duplicate {
			return nil, fmt.Errorf("duplicate cli field %q", tag)
		}
		seen[tag] = struct{}{}
		fields = append(fields, field{index: index, goName: structField.Name, name: tag, help: structField.Tag.Get("help"), typeOf: structField.Type})
	}
	return fields, nil
}

func structValue(target any) (reflect.Value, reflect.Type, error) {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("configuration must be a non-nil pointer to a struct")
	}
	return value.Elem(), value.Elem().Type(), nil
}

func supportedType(typeOf reflect.Type) bool {
	if reflect.PointerTo(typeOf).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return true
	}
	if typeOf == reflect.TypeFor[time.Duration]() || typeOf.Kind() == reflect.Bool || typeOf.Kind() == reflect.String || typeOf.Kind() >= reflect.Int && typeOf.Kind() <= reflect.Float64 || typeOf.Kind() >= reflect.Uint && typeOf.Kind() <= reflect.Uint64 {
		return true
	}
	return typeOf.Kind() == reflect.Slice && typeOf.Elem().Kind() == reflect.String
}

func setField(destination reflect.Value, values []string) error {
	if destination.Kind() == reflect.Slice {
		result := reflect.MakeSlice(destination.Type(), len(values), len(values))
		for index, raw := range values {
			result.Index(index).SetString(raw)
		}
		destination.Set(result)
		return nil
	}
	if len(values) != 1 {
		return fmt.Errorf("accepts one value")
	}
	raw := values[0]
	if destination.CanAddr() {
		if unmarshaler, ok := destination.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(raw))
		}
	}
	switch destination.Kind() {
	case reflect.String:
		destination.SetString(raw)
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		destination.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if destination.Type() == reflect.TypeFor[time.Duration]() {
			value, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			destination.SetInt(int64(value))
			return nil
		}
		value, err := strconv.ParseInt(raw, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetFloat(value)
	default:
		return fmt.Errorf("unsupported type %s", destination.Type())
	}
	return nil
}

func validate(value reflect.Value) error {
	if candidate, ok := value.Addr().Interface().(validator); ok {
		if err := candidate.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateChoice(value reflect.Value, field field) error {
	provider, ok := value.Addr().Interface().(choiceProvider)
	if !ok {
		return nil
	}
	choices := provider.FieldChoices(field.goName)
	if len(choices) == 0 {
		return nil
	}
	actual := fmt.Sprint(value.Field(field.index).Interface())
	if !slices.Contains(choices, actual) {
		return fmt.Errorf("must be one of %s", strings.Join(choices, ", "))
	}
	return nil
}

func formatValue(value reflect.Value) string {
	if value.Kind() == reflect.Slice {
		items := make([]string, value.Len())
		for index := range value.Len() {
			items[index] = value.Index(index).String()
		}
		return strings.Join(items, ",")
	}
	return fmt.Sprint(value.Interface())
}
