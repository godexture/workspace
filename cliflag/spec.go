package cliflag

import (
	"fmt"
	"strings"
)

type Spec struct {
	Name   string
	Values map[string]string
}

func ParseSpec(input string) (Spec, error) {
	name, options, found, err := splitFirst(input, ':')
	if err != nil || name == "" {
		return Spec{}, fmt.Errorf("invalid specification %q", input)
	}
	spec := Spec{Name: name, Values: map[string]string{}}
	if !found {
		return spec, nil
	}
	items, err := splitEscaped(options, ',')
	if err != nil {
		return Spec{}, err
	}
	for _, item := range items {
		key, value, found, err := splitFirst(item, '=')
		if err != nil || !found || key == "" {
			return Spec{}, fmt.Errorf("invalid option %q", item)
		}
		if _, exists := spec.Values[key]; exists {
			return Spec{}, fmt.Errorf("duplicate option %q", key)
		}
		spec.Values[key] = value
	}
	return spec, nil
}

func splitFirst(input string, separator rune) (string, string, bool, error) {
	var value strings.Builder
	escaped := false
	for index, char := range input {
		if escaped {
			value.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == separator {
			return value.String(), input[index+1:], true, nil
		}
		value.WriteRune(char)
	}
	if escaped {
		return "", "", false, fmt.Errorf("trailing escape in %q", input)
	}
	return value.String(), "", false, nil
}

func splitEscaped(input string, separator rune) ([]string, error) {
	var result []string
	var value strings.Builder
	escaped := false
	for _, char := range input {
		if escaped {
			value.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == separator {
			result = append(result, value.String())
			value.Reset()
			continue
		}
		value.WriteRune(char)
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape in %q", input)
	}
	return append(result, value.String()), nil
}
