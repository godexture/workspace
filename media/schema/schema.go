// Package schema defines open typed data schemas and their marker identities.
package schema

import (
	"fmt"
	"reflect"
)

// ID is the stable identity of a schema. It is derived from a marker type,
// never from the payload type.
type ID struct{ canonical string }

func (id ID) IsZero() bool   { return id.canonical == "" }
func (id ID) String() string { return id.canonical }
func (id ID) PackagePath() string {
	for index := len(id.canonical) - 1; index >= 0; index-- {
		if id.canonical[index] == '.' {
			return id.canonical[:index]
		}
	}
	return ""
}
func (id ID) Name() string {
	for index := len(id.canonical) - 1; index >= 0; index-- {
		if id.canonical[index] == '.' {
			return id.canonical[index+1:]
		}
	}
	return id.canonical
}

// Traits are optional typed operations used when a data path needs the
// operation. A linear path never needs to call Size or Time just to transport
// a value.
type Traits[T any] struct {
	Fork func(T) T
	Drop func(T)
	Size func(T) int
	Time func(T) (int64, bool)
}

// Type is the typed schema handle retained by code that knows T.
type Type[T any] struct {
	descriptor Descriptor
	traits     Traits[T]
	problem    error
}

// Define derives a schema identity from ID while retaining typed traits.
func Define[IDMarker, T any](traits Traits[T]) Type[T] {
	identity, problem := identityWithProblem[IDMarker]()
	return Type[T]{
		descriptor: Descriptor{identity: identity, payload: reflect.TypeFor[T]()},
		traits:     traits,
		problem:    problem,
	}
}

// IdentityOf returns the marker identity without registering a schema.
func IdentityOf[IDMarker any]() ID {
	identity, _ := identityWithProblem[IDMarker]()
	return identity
}

func identityWithProblem[IDMarker any]() (ID, error) {
	typ := reflect.TypeFor[IDMarker]()
	if typ == nil || typ.Kind() == reflect.Interface || typ.Name() == "" || typ.PkgPath() == "" {
		return ID{}, fmt.Errorf("marker must be a named concrete type declared by a package")
	}
	return ID{canonical: typ.PkgPath() + "." + typ.Name()}, nil
}

func (t Type[T]) Valid() bool            { return !t.descriptor.identity.IsZero() }
func (t Type[T]) Identity() ID           { return t.descriptor.identity }
func (t Type[T]) Traits() Traits[T]      { return t.traits }
func (t Type[T]) Descriptor() Descriptor { return t.descriptor }

// Problem returns the marker validation problem, if any. Flow ports carry it
// into host diagnostics instead of silently reducing an invalid marker to a
// zero identity.
func (t Type[T]) Problem() error { return t.problem }

func (t Type[T]) Fork(value T) T {
	if t.traits.Fork != nil {
		return t.traits.Fork(value)
	}
	return value
}

func (t Type[T]) Drop(value T) {
	if t.traits.Drop != nil {
		t.traits.Drop(value)
	}
}

func (t Type[T]) Size(value T) (int, bool) {
	if t.traits.Size == nil {
		return 0, false
	}
	return t.traits.Size(value), true
}

func (t Type[T]) Time(value T) (int64, bool) {
	if t.traits.Time == nil {
		return 0, false
	}
	return t.traits.Time(value)
}

// Descriptor is the erased schema representation held by ports and
// components. Typed operations remain on Type[T] so an erased descriptor
// cannot guess how to clone, release, measure, or timestamp an arbitrary T.
type Descriptor struct {
	identity ID
	payload  reflect.Type
}

func (d Descriptor) Valid() bool           { return !d.identity.IsZero() && d.payload != nil }
func (d Descriptor) Identity() ID          { return d.identity }
func (d Descriptor) Payload() reflect.Type { return d.payload }
