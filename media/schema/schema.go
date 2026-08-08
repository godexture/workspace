// Package schema defines open typed data schemas and their marker identities.
package schema

import (
	"errors"
	"reflect"

	"github.com/godexture/godec/internal/marker"
)

// ID is the stable identity of a schema. It is derived from a marker type,
// never from the payload type.
type ID struct{ canonical string }

func (id ID) IsZero() bool        { return id.canonical == "" }
func (id ID) String() string      { return id.canonical }
func (id ID) PackagePath() string { return marker.PackagePath(id.canonical) }
func (id ID) Name() string        { return marker.Name(id.canonical) }

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
}

// Define derives a schema identity from ID while retaining typed traits.
func Define[IDMarker, T any](traits Traits[T]) Type[T] {
	identity, problem := identityWithProblem[IDMarker]()
	descriptor := Descriptor{
		identity: identity,
		payload:  reflect.TypeFor[T](),
		problem:  errorText(problem),
	}
	return Type[T]{
		descriptor: descriptor,
		traits:     traits,
	}
}

func errorText(problem error) string {
	if problem == nil {
		return ""
	}
	return problem.Error()
}

// IdentityOf returns the marker identity without registering a schema.
func IdentityOf[IDMarker any]() ID {
	identity, _ := identityWithProblem[IDMarker]()
	return identity
}

func identityWithProblem[IDMarker any]() (ID, error) {
	canonical, err := marker.Canonical[IDMarker]()
	if err != nil {
		return ID{}, err
	}
	return ID{canonical: canonical}, nil
}

func (t Type[T]) Valid() bool            { return t.descriptor.Valid() }
func (t Type[T]) Identity() ID           { return t.descriptor.identity }
func (t Type[T]) Traits() Traits[T]      { return t.traits }
func (t Type[T]) Descriptor() Descriptor { return t.descriptor }

// Problem returns the marker validation problem, if any. Flow ports carry it
// into host diagnostics instead of silently reducing an invalid marker to a
// zero identity.
func (t Type[T]) Problem() error { return t.descriptor.Problem() }

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

// noCompare keeps Descriptor non-comparable so schema equality always goes
// through marker identity and payload type instead of becoming accidental
// struct equality as descriptor implementation details evolve.
type noCompare [0]func()

// Descriptor is the erased schema representation held by ports and
// components. Typed operations remain on Type[T] so an erased descriptor
// cannot guess how to clone, release, measure, or timestamp an arbitrary T.
type Descriptor struct {
	_        noCompare
	identity ID
	payload  reflect.Type
	problem  string
}

func (d Descriptor) Valid() bool {
	return !d.identity.IsZero() && d.payload != nil && d.problem == ""
}

func (d Descriptor) Identity() ID          { return d.identity }
func (d Descriptor) Payload() reflect.Type { return d.payload }

// Problem returns the schema construction problem, if any.
func (d Descriptor) Problem() error {
	if d.problem == "" {
		return nil
	}
	return errors.New(d.problem)
}
