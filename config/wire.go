package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

func (s Schema[C]) decodeJSON(value string) (C, error) {
	var decoded C
	if !s.Valid() {
		return decoded, s.Err()
	}
	if strings.TrimSpace(value) == "null" {
		return decoded, fmt.Errorf("nested value must be a JSON object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &raw); err != nil || raw == nil {
		return decoded, fmt.Errorf("nested value must be a JSON object")
	}
	var factoryItems []diagnostic.Item
	decoded, factoryItems = s.defaultValue()
	if len(factoryItems) != 0 {
		return decoded, diagnosticError(factoryItems)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field, ok := s.field(key)
		if !ok {
			return decoded, withDecodePath(key, fmt.Errorf("unknown nested field %q", key))
		}
		decodedValue, err := field.decode(jsonValueText(raw[key]))
		if err != nil {
			return decoded, withDecodePath(key, err)
		}
		if err := field.write(&decoded, decodedValue); err != nil {
			return decoded, withDecodePath(key, err)
		}
	}
	return decoded, nil
}

func (s Schema[C]) encodeJSON(value C) string {
	parts := make([]string, 0, len(s.fields))
	for _, field := range s.fields {
		if field.description.Secret {
			continue
		}
		fieldValue, err := field.read(&value)
		if err != nil || field.encode == nil {
			return "<invalid>"
		}
		encoded := surfaceJSON(field.encode(fieldValue))
		key, marshalErr := json.Marshal(field.id)
		if marshalErr != nil {
			return "<invalid>"
		}
		parts = append(parts, string(key)+":"+encoded)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func surfaceJSON(encoded string) string {
	if json.Valid([]byte(encoded)) {
		return encoded
	}
	quoted, err := json.Marshal(encoded)
	if err != nil {
		return "null"
	}
	return string(quoted)
}

type decodePathError struct {
	path []string
	err  error
}

type decodeError struct {
	code    string
	message string
}

func (e decodeError) Error() string { return e.message }

func (e decodeError) decodeCode() string { return e.code }

type codedDecodeError interface {
	decodeCode() string
}

func decodeDiagnosticCode(err error) string {
	var coded codedDecodeError
	if errors.As(err, &coded) && coded != nil && coded.decodeCode() != "" {
		return coded.decodeCode()
	}
	return codeInvalidInput
}

func (e *decodePathError) Error() string { return e.err.Error() }

func (e *decodePathError) Unwrap() error { return e.err }

func withDecodePath(field string, err error) error {
	if err == nil {
		return nil
	}
	var pathErr *decodePathError
	if errors.As(err, &pathErr) {
		path := append([]string{field}, pathErr.path...)
		return &decodePathError{path: path, err: err}
	}
	return &decodePathError{path: []string{field}, err: err}
}

func decodePath(err error) []string {
	var pathErr *decodePathError
	if !errors.As(err, &pathErr) || pathErr == nil {
		return nil
	}
	return append([]string(nil), pathErr.path...)
}

func jsonValueText(value []byte) string {
	var text string
	if len(value) != 0 && value[0] == '"' && json.Unmarshal(value, &text) == nil {
		return text
	}
	return string(value)
}
