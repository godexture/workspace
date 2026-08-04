// Package property defines immutable, open-ended control-plane properties.
package property

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/godexture/godec/internal/marker"
	"github.com/godexture/godec/internal/snapshot"
)

var ErrPropertyType = errors.New("property value has the wrong type for its key")

type ID struct{ canonical string }

func (id ID) IsZero() bool   { return id.canonical == "" }
func (id ID) String() string { return id.canonical }

type Key[T any] struct {
	id        ID
	valueType reflect.Type
	clone     func(T) T
	problem   string
}

// Define creates an open property key. Reference-valued properties must
// provide a clone function; immutable values use a shallow value copy.
func Define[Marker any, T any](clones ...func(T) T) Key[T] {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return Key[T]{problem: "property " + err.Error()}
	}
	key := Key[T]{id: ID{canonical: canonical}, valueType: reflect.TypeFor[T]()}
	if len(clones) > 1 || (len(clones) == 1 && clones[0] == nil) {
		key.problem = "property clone must be supplied at most once and must not be nil"
		return key
	}
	switch {
	case len(clones) == 1:
		key.clone = clones[0]
	case snapshot.NeedsClone(key.valueType):
		key.problem = fmt.Sprintf("property key %s requires a declared clone for reference-valued type %s", canonical, key.valueType)
	default:
		key.clone = func(value T) T { return value }
	}
	return key
}

func IdentityOf[Marker any]() ID {
	return Define[Marker, struct{}]().ID()
}

func (k Key[T]) Valid() bool             { return !k.id.IsZero() && k.valueType != nil && k.clone != nil }
func (k Key[T]) ID() ID                  { return k.id }
func (k Key[T]) ValueType() reflect.Type { return k.valueType }

// Problem returns the key construction problem, if any.
func (k Key[T]) Problem() error {
	if k.problem == "" {
		return nil
	}
	return errors.New(k.problem)
}

func (k Key[T]) Get(set Set) (T, bool) {
	entry, ok := set.values[k.id]
	if !ok || entry.typeOf != k.valueType {
		var zero T
		return zero, false
	}
	value, ok := k.cloneAny(entry.value)
	if !ok {
		var zero T
		return zero, false
	}
	return value.(T), true
}

func (k Key[T]) Set(set Set, value T) (Set, error) {
	return set.With(k, value)
}

type keyLike interface {
	ID() ID
	ValueType() reflect.Type
	Problem() error
	cloneAny(any) (any, bool)
}

type entry struct {
	typeOf reflect.Type
	value  any
	clone  func(any) (any, bool)
}

// Set is immutable. With and Delete return a new set and never modify their
// receiver. Values are copied at the boundary for common mutable byte data.
type Set struct{ values map[ID]entry }

func New() Set { return Set{values: make(map[ID]entry)} }

func (s Set) With(key keyLike, value any) (Set, error) {
	if key == nil {
		return s, errors.New("invalid property key")
	}
	if problem := key.Problem(); problem != nil {
		return s, problem
	}
	if key.ID().IsZero() || key.ValueType() == nil {
		return s, errors.New("invalid property key")
	}
	cloned, ok := key.cloneAny(value)
	if !ok {
		return s, ErrPropertyType
	}
	values := cloneEntries(s.values)
	values[key.ID()] = entry{typeOf: key.ValueType(), value: cloned, clone: key.cloneAny}
	return Set{values: values}, nil
}

func Put[T any](set Set, key Key[T], value T) (Set, error) {
	return key.Set(set, value)
}

func (s Set) Delete(key ID) Set {
	if s.values == nil {
		return s
	}
	values := cloneEntries(s.values)
	delete(values, key)
	return Set{values: values}
}

func (s Set) Len() int { return len(s.values) }

func (s Set) Lookup(key ID) (Value, bool) {
	entry, ok := s.values[key]
	if !ok {
		return Value{}, false
	}
	value, ok := entry.clone(entry.value)
	if !ok {
		return Value{}, false
	}
	return Value{id: key, typeOf: entry.typeOf, value: value, clone: entry.clone}, true
}

func (s Set) Keys() []ID {
	result := make([]ID, 0, len(s.values))
	for key := range s.values {
		result = append(result, key)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].String() < result[right].String() })
	return result
}

type Value struct {
	id     ID
	typeOf reflect.Type
	value  any
	clone  func(any) (any, bool)
}

func (v Value) ID() ID             { return v.id }
func (v Value) Type() reflect.Type { return v.typeOf }
func (v Value) Any() any {
	if v.clone == nil {
		return nil
	}
	value, ok := v.clone(v.value)
	if !ok {
		return nil
	}
	return value
}

func cloneEntries(source map[ID]entry) map[ID]entry {
	result := make(map[ID]entry, len(source))
	for key, value := range source {
		cloned, ok := value.clone(value.value)
		if !ok {
			continue
		}
		result[key] = entry{typeOf: value.typeOf, value: cloned, clone: value.clone}
	}
	return result
}

func (k Key[T]) cloneAny(value any) (any, bool) {
	if k.clone == nil {
		return nil, false
	}
	typed, ok := value.(T)
	if !ok {
		return nil, false
	}
	return k.clone(typed), true
}
