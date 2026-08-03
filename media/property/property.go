// Package property defines immutable, open-ended control-plane properties.
package property

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

var ErrPropertyType = errors.New("property value has the wrong type for its key")

type ID struct{ canonical string }

func (id ID) IsZero() bool   { return id.canonical == "" }
func (id ID) String() string { return id.canonical }

type Key[T any] struct {
	id        ID
	valueType reflect.Type
}

func Define[Marker any, T any]() Key[T] {
	typ := reflect.TypeFor[Marker]()
	if typ == nil || typ.Kind() == reflect.Interface || typ.Name() == "" || typ.PkgPath() == "" {
		return Key[T]{}
	}
	return Key[T]{
		id:        ID{canonical: typ.PkgPath() + "." + typ.Name()},
		valueType: reflect.TypeFor[T](),
	}
}

func IdentityOf[Marker any]() ID {
	return Define[Marker, struct{}]().ID()
}

func (k Key[T]) Valid() bool             { return !k.id.IsZero() && k.valueType != nil }
func (k Key[T]) ID() ID                  { return k.id }
func (k Key[T]) ValueType() reflect.Type { return k.valueType }

func (k Key[T]) Get(set Set) (T, bool) {
	entry, ok := set.values[k.id]
	if !ok || entry.typeOf != k.valueType {
		var zero T
		return zero, false
	}
	value, ok := entry.value.(T)
	return cloneValue(value).(T), ok
}

func (k Key[T]) Set(set Set, value T) (Set, error) {
	return set.With(k, value)
}

type keyLike interface {
	ID() ID
	ValueType() reflect.Type
}

type entry struct {
	typeOf reflect.Type
	value  any
}

// Set is immutable. With and Delete return a new set and never modify their
// receiver. Values are copied at the boundary for common mutable byte data.
type Set struct{ values map[ID]entry }

func New() Set { return Set{values: make(map[ID]entry)} }

func (s Set) With(key keyLike, value any) (Set, error) {
	if key == nil || key.ID().IsZero() || key.ValueType() == nil {
		return s, errors.New("invalid property key")
	}
	if value == nil {
		return s, ErrPropertyType
	}
	valueType := reflect.TypeOf(value)
	if valueType != key.ValueType() {
		return s, fmt.Errorf("%w: got %s, want %s", ErrPropertyType, valueType, key.ValueType())
	}
	values := cloneEntries(s.values)
	values[key.ID()] = entry{typeOf: valueType, value: cloneValue(value)}
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
	return Value{id: key, typeOf: entry.typeOf, value: cloneValue(entry.value)}, true
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
}

func (v Value) ID() ID             { return v.id }
func (v Value) Type() reflect.Type { return v.typeOf }
func (v Value) Any() any           { return cloneValue(v.value) }

func cloneEntries(source map[ID]entry) map[ID]entry {
	result := make(map[ID]entry, len(source))
	for key, value := range source {
		result[key] = entry{typeOf: value.typeOf, value: cloneValue(value.value)}
	}
	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...)
	case string:
		return typed
	}
	return value
}
