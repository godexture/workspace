package cliflag

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
)

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
