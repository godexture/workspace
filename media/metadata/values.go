package metadata

import "github.com/godexture/godec/media/key"

// Values returns every value stored under declaration, in document order. A
// key may legitimately carry several values, so this never collapses them.
func Values[T any](document Document, declaration key.Key[T]) []T {
	erased := declaration.Erased()
	if !erased.Valid() {
		return nil
	}
	var result []T
	for _, entry := range document.entries {
		if entry.Key() != erased.ID() || entry.ValueType() != erased.ValueType() {
			continue
		}
		value, ok := entry.declaration.Clone(entry.value)
		if !ok {
			continue
		}
		typed, ok := value.(T)
		if !ok {
			continue
		}
		result = append(result, typed)
	}
	return result
}

// First returns the first value stored under declaration.
func First[T any](document Document, declaration key.Key[T]) (T, bool) {
	values := Values(document, declaration)
	if len(values) == 0 {
		var zero T
		return zero, false
	}
	return values[0], true
}
