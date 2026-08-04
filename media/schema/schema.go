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

// Queue is the minimal typed product created by an erased schema factory.
// Its concrete implementation is intentionally private to this package.
type Queue[T any] interface {
	Push(T) bool
	Pop() (T, bool)
}

// Fanout is the minimal typed fan-out product created by an erased schema
// factory. Its concrete implementation is intentionally private to this
// package.
type Fanout[T any] interface {
	Outputs() int
	Split(T) []T
	Drop(T)
}

// Define derives a schema identity from ID while retaining typed traits.
func Define[IDMarker, T any](traits Traits[T]) Type[T] {
	identity, problem := identityWithProblem[IDMarker]()
	descriptor := Descriptor{
		identity: identity,
		payload:  reflect.TypeFor[T](),
		problem:  errorText(problem),
	}
	if problem == nil {
		descriptor.factory = &descriptorFactory{
			newPipe: func() any { return &pipe[T]{} },
			newTee:  func(outputs int) (any, error) { return newFanout(outputs, traits.Fork, traits.Drop) },
		}
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

// noCompare makes Descriptor non-comparable. Two Define calls for the same
// marker and payload capture different factory closures, so == would report
// them as different schemas while Identity reports them as the same one.
// Equality must go through Identity.
type noCompare [0]func()

// Descriptor is the erased schema representation held by ports and
// components. Typed operations remain on Type[T] so an erased descriptor
// cannot guess how to clone, release, measure, or timestamp an arbitrary T.
type Descriptor struct {
	_        noCompare
	identity ID
	payload  reflect.Type
	problem  string
	factory  *descriptorFactory
}

type descriptorFactory struct {
	newPipe func() any
	newTee  func(int) (any, error)
}

func (d Descriptor) Valid() bool {
	return !d.identity.IsZero() && d.payload != nil && d.factory != nil
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

// NewPipe creates an erased queue product. The caller asserts the result to
// Queue[T] once while opening a typed endpoint; item operations remain typed.
func (d Descriptor) NewPipe() (any, error) {
	if d.factory == nil || d.factory.newPipe == nil {
		if problem := d.Problem(); problem != nil {
			return nil, problem
		}
		return nil, errors.New("schema has no pipe factory")
	}
	return d.factory.newPipe(), nil
}

// NewTee creates an erased fan-out product. The caller asserts the result to
// Fanout[T] once while opening a typed endpoint; item operations remain typed.
func (d Descriptor) NewTee(outputs int) (any, error) {
	if d.factory == nil || d.factory.newTee == nil {
		if problem := d.Problem(); problem != nil {
			return nil, problem
		}
		return nil, errors.New("schema has no tee factory")
	}
	return d.factory.newTee(outputs)
}

type pipe[T any] struct{ values []T }

func (p *pipe[T]) Push(value T) bool {
	if p == nil {
		return false
	}
	p.values = append(p.values, value)
	return true
}

func (p *pipe[T]) Pop() (T, bool) {
	if p == nil || len(p.values) == 0 {
		var zero T
		return zero, false
	}
	value := p.values[0]
	var zero T
	p.values[0] = zero
	p.values = p.values[1:]
	return value, true
}

type fanout[T any] struct {
	outputs int
	fork    func(T) T
	drop    func(T)
}

func newFanout[T any](outputs int, fork func(T) T, drop func(T)) (Fanout[T], error) {
	if outputs <= 0 {
		return nil, errors.New("fan-out output count must be positive")
	}
	return &fanout[T]{outputs: outputs, fork: fork, drop: drop}, nil
}

func (f *fanout[T]) Outputs() int {
	if f == nil {
		return 0
	}
	return f.outputs
}

func (f *fanout[T]) Split(value T) []T {
	if f == nil || f.outputs == 0 {
		return nil
	}
	result := make([]T, f.outputs)
	result[0] = value
	for index := 1; index < f.outputs; index++ {
		if f.fork != nil {
			result[index] = f.fork(value)
		} else {
			result[index] = value
		}
	}
	return result
}

func (f *fanout[T]) Drop(value T) {
	if f != nil && f.drop != nil {
		f.drop(value)
	}
}
