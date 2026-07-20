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
	parts, err := splitEscaped(input, ':')
	if err != nil {
		return Spec{}, err
	}
	if len(parts) > 2 || parts[0] == "" {
		return Spec{}, fmt.Errorf("invalid specification %q", input)
	}
	spec := Spec{Name: parts[0], Values: map[string]string{}}
	if len(parts) == 1 {
		return spec, nil
	}
	for _, item := range splitComma(parts[1]) {
		pair, err := splitEscaped(item, '=')
		if err != nil {
			return Spec{}, err
		}
		if len(pair) != 2 || pair[0] == "" {
			return Spec{}, fmt.Errorf("invalid option %q", item)
		}
		if _, exists := spec.Values[pair[0]]; exists {
			return Spec{}, fmt.Errorf("duplicate option %q", pair[0])
		}
		spec.Values[pair[0]] = pair[1]
	}
	return spec, nil
}

func splitComma(input string) []string {
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
		if char == ',' {
			result = append(result, value.String())
			value.Reset()
			continue
		}
		value.WriteRune(char)
	}
	if escaped {
		value.WriteByte('\\')
	}
	return append(result, value.String())
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
