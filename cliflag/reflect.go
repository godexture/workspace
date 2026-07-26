package cliflag

import (
	"encoding"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

func fieldsFor(typeOf reflect.Type) ([]field, error) {
	fields := make([]field, 0, typeOf.NumField())
	seen := map[string]struct{}{}
	indexByName := map[string]int{}
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
		dependsOn, err := parseDependency(structField)
		if err != nil {
			return nil, err
		}
		checks, err := parseChecks(structField)
		if err != nil {
			return nil, err
		}
		indexByName[tag] = len(fields)
		fields = append(fields, field{
			index: index, goName: structField.Name, name: tag, help: structField.Tag.Get("help"),
			typeOf: structField.Type, dependsOn: dependsOn, checks: checks,
		})
	}
	for index := range fields {
		field := &fields[index]
		if field.dependsOn == nil {
			continue
		}
		if field.dependsOn.Field == field.name {
			return nil, fmt.Errorf("field %s cannot depend on itself", field.goName)
		}
		target, ok := indexByName[field.dependsOn.Field]
		if !ok {
			return nil, fmt.Errorf("field %s depends on unknown field %q", field.goName, field.dependsOn.Field)
		}
		if fields[target].typeOf.Kind() == reflect.Slice {
			return nil, fmt.Errorf("field %s cannot depend on slice-typed field %q", field.goName, field.dependsOn.Field)
		}
		field.dependsOnIndex = fields[target].index
	}
	return fields, nil
}

func parseDependency(structField reflect.StructField) (*FieldDependency, error) {
	raw, exists := structField.Tag.Lookup("depends-on")
	if !exists {
		return nil, nil
	}
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("field %s has an invalid depends-on tag", structField.Name)
	}
	return &FieldDependency{Field: parts[0], Values: strings.Split(parts[1], ",")}, nil
}

func parseChecks(structField reflect.StructField) ([]string, error) {
	raw, exists := structField.Tag.Lookup("check")
	if !exists {
		return nil, nil
	}
	if !numericKind(structField.Type.Kind()) {
		return nil, fmt.Errorf("field %s has a check tag but is not numeric", structField.Name)
	}
	checks := strings.Split(raw, ",")
	for _, check := range checks {
		switch check {
		case "positive", "finite", "nonnegative":
		default:
			return nil, fmt.Errorf("field %s has unknown check %q", structField.Name, check)
		}
	}
	return checks, nil
}

func numericKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
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
	if typeOf == reflect.TypeFor[time.Duration]() {
		return true
	}
	switch typeOf.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
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

func dependsOnSatisfied(structValue reflect.Value, field field) bool {
	if field.dependsOn == nil {
		return true
	}
	actual := fmt.Sprint(structValue.Field(field.dependsOnIndex).Interface())
	return slices.Contains(field.dependsOn.Values, actual)
}

func validateChecks(value reflect.Value, fields []field) error {
	for _, field := range fields {
		if len(field.checks) == 0 || !dependsOnSatisfied(value, field) {
			continue
		}
		fieldValue := value.Field(field.index)
		var asFloat float64
		switch fieldValue.Kind() {
		case reflect.Float32, reflect.Float64:
			asFloat = fieldValue.Float()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			asFloat = float64(fieldValue.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			asFloat = float64(fieldValue.Uint())
		}
		for _, check := range field.checks {
			switch check {
			case "finite":
				if math.IsNaN(asFloat) || math.IsInf(asFloat, 0) {
					return fmt.Errorf("%s must be finite", field.name)
				}
			case "positive":
				if !(asFloat > 0) {
					return fmt.Errorf("%s must be positive", field.name)
				}
			case "nonnegative":
				if !(asFloat >= 0) {
					return fmt.Errorf("%s must be non-negative", field.name)
				}
			}
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
