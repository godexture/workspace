package property

import (
	"errors"
	"reflect"

	"github.com/godexture/godec/media/key"
)

// Encoder returns the deterministic representation of a property value used
// by descriptor fingerprints.
type Encoder[T any] func(T) ([]byte, error)

// Key is a typed property key with a required canonical encoder.
type Key[T any] struct {
	erased    key.Erased
	canonical Encoder[T]
	problem   string
}

// Define creates an open property key. Every property has to declare a
// canonical encoder because properties participate in descriptor fingerprints.
func Define[Marker, T any](encoder Encoder[T], clones ...func(T) T) Key[T] {
	declared := key.Define[Marker, T](clones...)
	result := Key[T]{erased: declared.Erased(), canonical: encoder}
	if encoder == nil {
		result.problem = "property key requires a canonical encoder"
	}
	return result
}

func (k Key[T]) Valid() bool             { return k.problem == "" && k.erased.Valid() }
func (k Key[T]) ID() key.ID              { return k.erased.ID() }
func (k Key[T]) ValueType() reflect.Type { return k.erased.ValueType() }

// Problem returns the key construction problem, if any.
func (k Key[T]) Problem() error {
	if problem := k.erased.Problem(); problem != nil {
		return problem
	}
	if k.problem == "" {
		return nil
	}
	return errors.New(k.problem)
}

// Erased returns the shared key declaration for property storage and
// composition.
func (k Key[T]) Erased() key.Erased { return k.erased }

// Canonical returns the key's deterministic encoding for value.
func (k Key[T]) Canonical(value T) ([]byte, error) {
	if problem := k.Problem(); problem != nil {
		return nil, problem
	}
	canonical, err := k.canonical(value)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), canonical...), nil
}

func (k Key[T]) Get(set Set) (T, bool) {
	entry, ok := set.values[k.ID()]
	if !ok || entry.declaration.ValueType() != k.ValueType() {
		var zero T
		return zero, false
	}
	value, ok := entry.declaration.Clone(entry.value)
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

func (k Key[T]) Set(set Set, value T) (Set, error) {
	return set.With(k, value)
}

func (Key[T]) propertyKey() {}

type keyLike interface {
	Erased() key.Erased
	Problem() error
	propertyKey()
}
