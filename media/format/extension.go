package format

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

// Extension is a canonical file-name hint. It is lower-case and does not
// include the leading dot.
type Extension string

func ParseExtension(value string) (Extension, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, ".")
	value = strings.ToLower(value)
	if value == "" {
		return "", errors.New("format extension is required")
	}
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsDigit(current) || current == '+' || current == '-' || current == '_' {
			continue
		}
		return "", errors.New("format extension contains an invalid character")
	}
	return Extension(value), nil
}

func (e Extension) Valid() bool {
	parsed, err := ParseExtension(string(e))
	return err == nil && parsed == e
}

func (e Extension) String() string { return string(e) }

func canonicalExtensions(values []string) ([]Extension, error) {
	result := make([]Extension, len(values))
	seen := make(map[Extension]struct{}, len(values))
	for index, value := range values {
		extension, err := ParseExtension(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[extension]; exists {
			return nil, errors.New("format extension is repeated")
		}
		seen[extension] = struct{}{}
		result[index] = extension
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}
