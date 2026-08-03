package cliflag

import (
	"fmt"
	"reflect"
	"strings"
)

func parseDependency(structField reflect.StructField) (*FieldDependency, error) {
	tag, exists := structField.Tag.Lookup("depends-on")
	if !exists {
		return nil, nil
	}
	parts := strings.SplitN(tag, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("invalid depends-on tag format on field %s", structField.Name)
	}
	values := strings.Split(parts[1], ",")
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid depends-on tag format on field %s", structField.Name)
		}
	}
	return &FieldDependency{
		Field:  parts[0],
		Values: values,
	}, nil
}
