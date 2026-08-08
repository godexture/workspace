package property

import (
	"bytes"
	"errors"
	"reflect"
	"sort"

	"github.com/godexture/godec/media/key"
)

type entry struct {
	declaration key.Erased
	value       any
	canonical   []byte
}

// Set is immutable. With and Delete return a new set and never modify their
// receiver.
type Set struct{ values map[key.ID]entry }

func New() Set { return Set{values: make(map[key.ID]entry)} }

func (s Set) With(declaration propertyDeclaration, value any) (Set, error) {
	if declaration == nil {
		return s, errors.New("invalid property key")
	}
	if problem := declaration.propertyProblem(); problem != nil {
		return s, problem
	}
	erased := declaration.Erased()
	if problem := erased.Problem(); problem != nil {
		return s, problem
	}
	if !erased.Valid() {
		return s, errors.New("invalid property key")
	}
	cloned, ok := erased.Clone(value)
	if !ok {
		return s, key.ErrType
	}
	canonical, err := declaration.propertyCanonical(cloned)
	if err != nil {
		return s, err
	}
	if existing, exists := s.values[erased.ID()]; exists && existing.declaration.ValueType() != erased.ValueType() {
		return s, errors.New("property identity is already bound to a different value type")
	}
	values := cloneEntries(s.values)
	values[erased.ID()] = entry{declaration: erased, value: cloned, canonical: append([]byte(nil), canonical...)}
	return Set{values: values}, nil
}

func Put[T any](set Set, declaration Key[T], value T) (Set, error) {
	return declaration.Set(set, value)
}

func (s Set) Delete(id key.ID) Set {
	if s.values == nil {
		return s
	}
	values := cloneEntries(s.values)
	delete(values, id)
	return Set{values: values}
}

func (s Set) Len() int { return len(s.values) }

func (s Set) Lookup(id key.ID) (Value, bool) {
	entry, ok := s.values[id]
	if !ok {
		return Value{}, false
	}
	value, ok := entry.declaration.Clone(entry.value)
	if !ok {
		return Value{}, false
	}
	return Value{declaration: entry.declaration, value: value}, true
}

func (s Set) Keys() []key.ID {
	result := make([]key.ID, 0, len(s.values))
	for id := range s.values {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result
}

// Equal compares the canonical property state without inspecting arbitrary
// Go values. A property's encoder defines semantic equality for planning.
func (s Set) Equal(other Set) bool {
	if len(s.values) != len(other.values) {
		return false
	}
	for id, left := range s.values {
		right, ok := other.values[id]
		if !ok || left.declaration.ValueType() != right.declaration.ValueType() || !bytes.Equal(left.canonical, right.canonical) {
			return false
		}
	}
	return true
}

// Value is a snapshot of one property entry.
type Value struct {
	declaration key.Erased
	value       any
}

func (v Value) ID() key.ID         { return v.declaration.ID() }
func (v Value) Type() reflect.Type { return v.declaration.ValueType() }
func (v Value) Any() any {
	value, ok := v.declaration.Clone(v.value)
	if !ok {
		return nil
	}
	return value
}

func cloneEntries(source map[key.ID]entry) map[key.ID]entry {
	result := make(map[key.ID]entry, len(source))
	for id, value := range source {
		cloned, ok := value.declaration.Clone(value.value)
		if !ok {
			continue
		}
		result[id] = entry{declaration: value.declaration, value: cloned, canonical: append([]byte(nil), value.canonical...)}
	}
	return result
}
