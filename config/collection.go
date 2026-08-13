package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

// Slice returns an ordered slice codec. Element order is part of the
// canonical representation.
func Slice[T any](inner Codec[T]) Codec[[]T] {
	result := NewCodec(CodecSpec[[]T]{
		Type: "slice<" + inner.description.Type + ">",
		Decode: func(value string) ([]T, error) {
			if strings.TrimSpace(value) == "null" {
				return nil, nil
			}
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				return nil, fmt.Errorf("slice must be a JSON array")
			}
			result := make([]T, len(raw))
			for index, item := range raw {
				decoded, err := inner.Decode(jsonValueText(item))
				if err != nil {
					return nil, withDecodePath(strconv.Itoa(index), fmt.Errorf("slice item %d: %w", index, err))
				}
				result[index] = decoded
			}
			return result, nil
		},
		Encode: func(value []T) string {
			if value == nil {
				return "null"
			}
			parts := make([]string, len(value))
			for index, item := range value {
				parts[index] = surfaceJSON(inner.Encode(item))
			}
			return "[" + strings.Join(parts, ",") + "]"
		},
		Canonical: func(value []T) ([]byte, error) {
			parts := make([][]byte, len(value))
			for index, item := range value {
				canonical, err := inner.Canonical(item)
				if err != nil {
					return nil, fmt.Errorf("slice item %d: %w", index, err)
				}
				parts[index] = canonical
			}
			return canonicalSequence("slice", parts...), nil
		},
		Clone: func(value []T) []T {
			if value == nil {
				return nil
			}
			result := make([]T, len(value))
			for index, item := range value {
				result[index] = inner.Clone(item)
			}
			return result
		},
		Normalize: func(value []T) ([]T, []diagnostic.Item) {
			result := make([]T, len(value))
			var items []diagnostic.Item
			for index, item := range value {
				normalized, childItems := inner.normalizeValue(item)
				result[index] = normalized
				for _, child := range childItems {
					child.Path = child.Path.Prefix(diagnostic.FieldPath(strconv.Itoa(index)))
					items = append(items, child)
				}
			}
			return result, items
		},
		Validate: func(value []T) []diagnostic.Item {
			var items []diagnostic.Item
			for index, item := range value {
				for _, child := range inner.validateValue(item) {
					child.Path = child.Path.Prefix(diagnostic.FieldPath(strconv.Itoa(index)))
					items = append(items, child)
				}
			}
			return items
		},
		Description: Description{Type: "slice<" + inner.description.Type + ">", Ordered: true},
	})
	if !inner.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-slice-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "slice inner codec must be valid", nil))
	}
	return result
}

// Map returns a map codec whose canonical entries are sorted by canonical key
// bytes. It is accepted only when both key and value codecs are canonical.
func Map[K comparable, V any](keyCodec Codec[K], valueCodec Codec[V]) Codec[map[K]V] {
	result := NewCodec(CodecSpec[map[K]V]{
		Type: "map<" + keyCodec.description.Type + "," + valueCodec.description.Type + ">",
		Decode: func(value string) (map[K]V, error) {
			if strings.TrimSpace(value) == "null" {
				return nil, nil
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(value), &raw); err != nil {
				return nil, fmt.Errorf("map must be a JSON object")
			}
			decoded := make(map[K]V, len(raw))
			for encodedKey, encodedValue := range raw {
				key, err := keyCodec.Decode(encodedKey)
				if err != nil {
					return nil, withDecodePath(encodedKey, fmt.Errorf("map key: %w", err))
				}
				item, err := valueCodec.Decode(jsonValueText(encodedValue))
				if err != nil {
					return nil, withDecodePath(encodedKey, fmt.Errorf("map value: %w", err))
				}
				decoded[key] = item
			}
			return decoded, nil
		},
		Encode: func(value map[K]V) string {
			if value == nil {
				return "null"
			}
			entries := sortedMapEntries(value, keyCodec)
			parts := make([]string, 0, len(entries))
			for _, entry := range entries {
				key, _ := json.Marshal(keyCodec.Encode(entry.key))
				parts = append(parts, string(key)+":"+surfaceJSON(valueCodec.Encode(entry.value)))
			}
			return "{" + strings.Join(parts, ",") + "}"
		},
		Canonical: func(value map[K]V) ([]byte, error) {
			entries := sortedMapEntries(value, keyCodec)
			parts := make([][]byte, 0, len(entries)*2)
			for _, entry := range entries {
				key, err := keyCodec.Canonical(entry.key)
				if err != nil {
					return nil, err
				}
				item, err := valueCodec.Canonical(entry.value)
				if err != nil {
					return nil, err
				}
				parts = append(parts, key, item)
			}
			return canonicalSequence("map", parts...), nil
		},
		Clone: func(value map[K]V) map[K]V {
			if value == nil {
				return nil
			}
			result := make(map[K]V, len(value))
			for key, item := range value {
				result[keyCodec.Clone(key)] = valueCodec.Clone(item)
			}
			return result
		},
		Normalize: func(value map[K]V) (map[K]V, []diagnostic.Item) {
			if value == nil {
				return nil, nil
			}
			result := make(map[K]V, len(value))
			var items []diagnostic.Item
			for key, item := range value {
				normalizedKey, keyItems := keyCodec.normalizeValue(key)
				normalizedValue, valueItems := valueCodec.normalizeValue(item)
				keyPath := keyCodec.Encode(key)
				for _, child := range keyItems {
					items = append(items, prefixItem(child, keyPath))
				}
				for _, child := range valueItems {
					items = append(items, prefixItem(child, keyPath))
				}
				if _, exists := result[normalizedKey]; exists {
					items = append(items, diagnostic.NewItem("config.map-key-collision", diagnostic.ErrorSeverity, diagnostic.FieldPath(keyPath), "map key normalization produced a duplicate key", nil))
					continue
				}
				result[normalizedKey] = normalizedValue
			}
			return result, items
		},
		Validate: func(value map[K]V) []diagnostic.Item {
			var items []diagnostic.Item
			for key, item := range value {
				for _, child := range keyCodec.validateValue(key) {
					items = append(items, prefixItem(child, keyCodec.Encode(key)))
				}
				for _, child := range valueCodec.validateValue(item) {
					items = append(items, prefixItem(child, keyCodec.Encode(key)))
				}
			}
			return items
		},
		Description: Description{Type: "map", Ordered: false},
	})
	if !keyCodec.Valid() || !valueCodec.Valid() {
		result = result.addConstruction(diagnostic.NewItem("config.invalid-map-codec", diagnostic.ErrorSeverity, diagnostic.Path{}, "map key and value codecs must be valid", nil))
	}
	return result
}

// Nested returns a codec backed by another typed schema. The nested schema's
// field IDs and canonical order define the nested representation.
func Nested[T any](schema Schema[T]) Codec[T] {
	result := NewCodec(CodecSpec[T]{
		Type:      "nested",
		Decode:    schema.decodeJSON,
		Encode:    schema.encodeJSON,
		Canonical: func(value T) ([]byte, error) { return schema.Canonical(value) },
		Clone: func(value T) T {
			cloned, _ := schema.snapshot(value)
			return cloned
		},
		Normalize: func(value T) (T, []diagnostic.Item) {
			normalized, _, items := schema.normalizeFields(value)
			return normalized, items
		},
		Validate: func(value T) []diagnostic.Item {
			return schema.validateValue(value)
		},
		Description: Description{Type: "nested"},
	})
	if !schema.Valid() {
		for _, item := range schema.Diagnostics() {
			result = result.addConstruction(item)
		}
	}
	return result
}

type mapEntry[K comparable, V any] struct {
	key   K
	value V
}

func sortedMapEntries[K comparable, V any](value map[K]V, codec Codec[K]) []mapEntry[K, V] {
	entries := make([]mapEntry[K, V], 0, len(value))
	for key, item := range value {
		entries = append(entries, mapEntry[K, V]{key: key, value: item})
	}
	sort.Slice(entries, func(left, right int) bool {
		leftCanonical, _ := codec.Canonical(entries[left].key)
		rightCanonical, _ := codec.Canonical(entries[right].key)
		return bytes.Compare(leftCanonical, rightCanonical) < 0
	})
	return entries
}
