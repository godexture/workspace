// Package schema defines open typed data schemas and their erased factories.
package schema

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

var (
	ErrInvalidMarker = errors.New("schema marker must be a named concrete type")
	ErrInvalidPipe   = errors.New("pipe limit must be non-negative")
	ErrInvalidTee    = errors.New("tee output count must be positive")
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

// Traits are optional operations used when a data path needs the operation.
// A linear path never needs to call Size or Fork just to transport a value.
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

// Define derives a schema identity from ID and captures typed factory
// closures while T is still known.
func Define[IDMarker, T any](traits Traits[T]) Type[T] {
	identity := identityOf[IDMarker]()
	result := Type[T]{traits: traits}
	result.descriptor = Descriptor{
		identity: identity,
		payload:  reflect.TypeFor[T](),
		newPipe:  func(limit int) (any, error) { return NewPipe[T](limit) },
		newTee:   func(outputs int) (any, error) { return NewTee[T](outputs, traits.Fork, traits.Drop) },
		newDrop: func(value any) {
			if traits.Drop != nil {
				traits.Drop(value.(T))
			}
		},
		newSize: func(value any) int {
			if traits.Size == nil {
				return 0
			}
			return traits.Size(value.(T))
		},
		newTime: func(value any) (int64, bool) {
			if traits.Time == nil {
				return 0, false
			}
			return traits.Time(value.(T))
		},
	}
	return result
}

// IdentityOf returns the marker identity without registering a schema.
func IdentityOf[IDMarker any]() ID { return identityOf[IDMarker]() }

func identityOf[IDMarker any]() ID {
	typ := reflect.TypeFor[IDMarker]()
	if typ == nil || typ.Kind() == reflect.Interface || typ.Name() == "" || typ.PkgPath() == "" {
		return ID{}
	}
	return ID{canonical: typ.PkgPath() + "." + typ.Name()}
}

func (t Type[T]) Valid() bool            { return !t.descriptor.identity.IsZero() }
func (t Type[T]) Identity() ID           { return t.descriptor.identity }
func (t Type[T]) Traits() Traits[T]      { return t.traits }
func (t Type[T]) Descriptor() Descriptor { return t.descriptor }

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

// Descriptor is the erased schema representation held by catalogs and
// planners. Its factories are registered only after T has been fixed.
type Descriptor struct {
	identity ID
	payload  reflect.Type
	newPipe  func(int) (any, error)
	newTee   func(int) (any, error)
	newDrop  func(any)
	newSize  func(any) int
	newTime  func(any) (int64, bool)
}

func (d Descriptor) Valid() bool           { return !d.identity.IsZero() && d.payload != nil }
func (d Descriptor) Identity() ID          { return d.identity }
func (d Descriptor) Payload() reflect.Type { return d.payload }

func (d Descriptor) NewPipe(limit int) (any, error) {
	if d.newPipe == nil {
		return nil, errors.New("schema has no pipe factory")
	}
	return d.newPipe(limit)
}

func (d Descriptor) NewTee(outputs int) (any, error) {
	if d.newTee == nil {
		return nil, errors.New("schema has no tee factory")
	}
	return d.newTee(outputs)
}

func (d Descriptor) Drop(value any) {
	if d.newDrop != nil {
		d.newDrop(value)
	}
}

func (d Descriptor) Size(value any) (int, bool) {
	if d.newSize == nil {
		return 0, false
	}
	return d.newSize(value), true
}

func (d Descriptor) Time(value any) (int64, bool) {
	if d.newTime == nil {
		return 0, false
	}
	return d.newTime(value)
}

// Catalog is an immutable set of erased schema descriptors.
type Catalog struct{ descriptors map[ID]Descriptor }

func NewCatalog(types ...Descriptor) (Catalog, error) {
	result := Catalog{descriptors: make(map[ID]Descriptor, len(types))}
	for _, descriptor := range types {
		if !descriptor.Valid() {
			return Catalog{}, errors.New("invalid schema descriptor")
		}
		if _, exists := result.descriptors[descriptor.identity]; exists {
			return Catalog{}, fmt.Errorf("schema identity %q is repeated", descriptor.identity)
		}
		result.descriptors[descriptor.identity] = descriptor
	}
	return result, nil
}

func (c Catalog) Lookup(identity ID) (Descriptor, bool) {
	descriptor, ok := c.descriptors[identity]
	return descriptor, ok
}

func (c Catalog) Has(identity ID) bool {
	_, ok := c.Lookup(identity)
	return ok
}

func (c Catalog) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(c.descriptors))
	for _, descriptor := range c.descriptors {
		result = append(result, descriptor)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Identity().String() < result[right].Identity().String()
	})
	return result
}

// Pipe is a small typed, synchronous factory product. It has no goroutine or
// channel and is useful for construction-time conformance tests.
type Pipe[T any] struct {
	limit  int
	values []T
}

func NewPipe[T any](limit int) (*Pipe[T], error) {
	if limit < 0 {
		return nil, ErrInvalidPipe
	}
	return &Pipe[T]{limit: limit}, nil
}

func (p *Pipe[T]) Push(value T) bool {
	if p == nil || (p.limit != 0 && len(p.values) >= p.limit) {
		return false
	}
	p.values = append(p.values, value)
	return true
}

func (p *Pipe[T]) Pop() (T, bool) {
	if p == nil || len(p.values) == 0 {
		var zero T
		return zero, false
	}
	value := p.values[0]
	p.values[0] = *new(T)
	p.values = p.values[1:]
	return value, true
}

func (p *Pipe[T]) Len() int {
	if p == nil {
		return 0
	}
	return len(p.values)
}

// Tee is a typed fan-out construction product. The actual scheduling and
// ownership policy belongs to flow/runtime milestones.
type Tee[T any] struct {
	outputs int
	fork    func(T) T
	drop    func(T)
}

func NewTee[T any](outputs int, fork func(T) T, drop func(T)) (*Tee[T], error) {
	if outputs <= 0 {
		return nil, ErrInvalidTee
	}
	return &Tee[T]{outputs: outputs, fork: fork, drop: drop}, nil
}

func (t *Tee[T]) Outputs() int {
	if t == nil {
		return 0
	}
	return t.outputs
}

func (t *Tee[T]) Split(value T) []T {
	if t == nil || t.outputs == 0 {
		return nil
	}
	result := make([]T, t.outputs)
	result[0] = value
	for index := 1; index < t.outputs; index++ {
		if t.fork != nil {
			result[index] = t.fork(value)
		} else {
			result[index] = value
		}
	}
	return result
}

func (t *Tee[T]) Drop(value T) {
	if t != nil && t.drop != nil {
		t.drop(value)
	}
}
