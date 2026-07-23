package cliflag

import (
	"fmt"
	"strings"
)

// Spec is a parsed "name[param=value,...]:key=value,..." specification.
// The bracketed segment is optional and, when present, is decoded
// separately from the colon segment: Parameters carries whatever a
// higher-order factory needs to determine its own shape (port count,
// or any other structural choice) before the regular per-instance
// Values are even resolved, since the concrete Configuration type those
// Values decode into may itself depend on Parameters. Parameters is nil
// when no "[...]" segment was present at all, distinguishing "no
// parameters given" from "parameters given but empty" ("name[]:...").
type Spec struct {
	Name       string
	Parameters map[string]string
	Values     map[string]string
}

func ParseSpec(input string) (Spec, error) {
	name, sep, rest, err := splitAt(input, '[', ':')
	if err != nil || name == "" {
		return Spec{}, fmt.Errorf("invalid specification %q", input)
	}
	spec := Spec{Name: name, Values: map[string]string{}}

	if sep == 0 {
		return spec, nil
	}

	if sep == '[' {
		paramsRaw, closeSep, tail, err := splitAt(rest, ']')
		if err != nil || closeSep != ']' {
			return Spec{}, fmt.Errorf("unterminated '[' in %q", input)
		}
		if paramsRaw == "" {
			spec.Parameters = map[string]string{}
		} else {
			params, err := parseAssignments(paramsRaw)
			if err != nil {
				return Spec{}, err
			}
			spec.Parameters = params
		}
		if tail == "" {
			return spec, nil
		}
		if tail[0] != ':' {
			return Spec{}, fmt.Errorf("expected ':' immediately after ']' in %q", input)
		}
		rest = tail[1:]
	}

	values, err := parseAssignments(rest)
	if err != nil {
		return Spec{}, err
	}
	spec.Values = values
	return spec, nil
}

func parseAssignments(input string) (map[string]string, error) {
	items, err := splitEscaped(input, ',')
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, item := range items {
		key, value, found, err := splitFirst(item, '=')
		if err != nil || !found || key == "" {
			return nil, fmt.Errorf("invalid option %q", item)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate option %q", key)
		}
		result[key] = value
	}
	return result, nil
}

func splitFirst(input string, separator rune) (string, string, bool, error) {
	before, sep, after, err := splitAt(input, separator)
	return before, after, sep != 0, err
}

// splitAt scans input for the first unescaped occurrence of any rune in
// separators (backslash-escapable, matching splitEscaped), returning the
// text before it, which separator matched (0 if none was found), and the
// text after it.
func splitAt(input string, separators ...rune) (before string, found rune, after string, err error) {
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
		for _, sep := range separators {
			if char == sep {
				return value.String(), char, input[index+1:], nil
			}
		}
		value.WriteRune(char)
	}
	if escaped {
		return "", 0, "", fmt.Errorf("trailing escape in %q", input)
	}
	return value.String(), 0, "", nil
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
