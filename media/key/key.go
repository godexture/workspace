// Package key defines open typed keys shared by media containers.
package key

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/internal/marker"
	"github.com/godexture/godec/internal/snapshot"
)

// ErrType reports a value whose type does not match its key.
var ErrType = errors.New("key value has the wrong type for its key")

// ID is the stable identity of an open key.
type ID struct{ canonical string }

func (id ID) IsZero() bool        { return id.canonical == "" }
func (id ID) String() string      { return id.canonical }
func (id ID) PackagePath() string { return marker.PackagePath(id.canonical) }
func (id ID) Name() string        { return marker.Name(id.canonical) }

// Erased is the shared, type-erased view of a declared key. Its fields are
// private so only a key declaration can provide the identity, value type, and
// snapshot rule that containers rely on.
type Erased struct {
	id        ID
	valueType reflect.Type
	clone     func(any) (any, bool)
	problem   string
}

func (e Erased) Valid() bool {
	return !e.id.IsZero() && e.valueType != nil && e.clone != nil && e.problem == ""
}
func (e Erased) ID() ID                  { return e.id }
func (e Erased) ValueType() reflect.Type { return e.valueType }

// Problem returns the declaration problem, if any.
func (e Erased) Problem() error {
	if e.problem == "" {
		return nil
	}
	return errors.New(e.problem)
}

// Clone returns a declared snapshot of value. It rejects values that do not
// have the declared payload type.
func (e Erased) Clone(value any) (any, bool) {
	if e.clone == nil {
		return nil, false
	}
	return e.clone(value)
}

// Key is a typed open key. Reference-valued keys must declare a clone so
// containers never guess how to snapshot mutable state.
type Key[T any] struct{ erased Erased }

// Define creates an open key in Marker's namespace.
func Define[Marker, T any](clones ...func(T) T) Key[T] {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return Key[T]{erased: Erased{problem: "key " + err.Error()}}
	}
	valueType := reflect.TypeFor[T]()
	erased := Erased{id: ID{canonical: canonical}, valueType: valueType}
	if len(clones) > 1 || (len(clones) == 1 && clones[0] == nil) {
		erased.problem = "key clone must be supplied at most once and must not be nil"
		return Key[T]{erased: erased}
	}
	if len(clones) == 1 {
		erased.clone = func(value any) (any, bool) {
			typed, ok := value.(T)
			if !ok {
				return nil, false
			}
			return clones[0](typed), true
		}
		return Key[T]{erased: erased}
	}
	if snapshot.NeedsClone(valueType) {
		erased.problem = fmt.Sprintf("key %s requires a declared clone for reference-valued type %s", canonical, valueType)
		return Key[T]{erased: erased}
	}
	erased.clone = func(value any) (any, bool) {
		typed, ok := value.(T)
		if !ok {
			return nil, false
		}
		return typed, true
	}
	return Key[T]{erased: erased}
}

func (k Key[T]) Valid() bool             { return k.erased.Valid() }
func (k Key[T]) ID() ID                  { return k.erased.ID() }
func (k Key[T]) ValueType() reflect.Type { return k.erased.ValueType() }
func (k Key[T]) Problem() error          { return k.erased.Problem() }

// Erased returns the key declaration for container storage and composition.
func (k Key[T]) Erased() Erased { return k.erased }
