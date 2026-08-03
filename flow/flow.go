// Package flow defines typed ports and lifecycle/ownership contracts without
// exposing queues, goroutines, channels, or a scheduler.
package flow

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/media/schema"
)

type Direction uint8

const (
	InputDirection Direction = iota + 1
	OutputDirection
)

type Multiplicity uint8

const (
	One Multiplicity = iota + 1
	OptionalMultiplicity
	ManyMultiplicity
)

type PortOption func(*portOptions)

type portOptions struct {
	required     bool
	multiplicity Multiplicity
}

// Required and Optional control whether a port must be connected. They are
// independent from multiplicity.
func Required() PortOption {
	return func(options *portOptions) { options.required = true }
}

func Optional() PortOption {
	return func(options *portOptions) { options.required = false }
}

// Many declares a port that may carry multiple connections. It may still be
// required; Required and multiplicity are separate axes.
func Many() PortOption {
	return func(options *portOptions) { options.multiplicity = ManyMultiplicity }
}

func WithMultiplicity(value Multiplicity) PortOption {
	return func(options *portOptions) { options.multiplicity = value }
}

// Port is an erased static port declaration. The schema identity is retained
// on the port; the typed constructors ensure it is created while T is known.
type Port struct {
	id            string
	direction     Direction
	schema        schema.ID
	schemaProblem error
	required      bool
	multiplicity  Multiplicity
}

func In[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, InputDirection, typ.Identity(), typ.Problem(), options...)
}

func Out[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, OutputDirection, typ.Identity(), typ.Problem(), options...)
}

func newPort(id string, direction Direction, identity schema.ID, schemaProblem error, options ...PortOption) Port {
	state := portOptions{required: true, multiplicity: One}
	for _, option := range options {
		if option != nil {
			option(&state)
		}
	}
	return Port{id: id, direction: direction, schema: identity, schemaProblem: schemaProblem, required: state.required, multiplicity: state.multiplicity}
}

func (p Port) ID() string                 { return p.id }
func (p Port) Direction() Direction       { return p.direction }
func (p Port) Schema() schema.ID          { return p.schema }
func (p Port) Required() bool             { return p.required }
func (p Port) Multiplicity() Multiplicity { return p.multiplicity }

// Shape is a static set of input and output ports. Dynamic topology is a
// planner phase and is intentionally absent here.
type Shape struct {
	Inputs  []Port
	Outputs []Port
}

func NewShape(inputs, outputs []Port) Shape {
	return Shape{Inputs: append([]Port(nil), inputs...), Outputs: append([]Port(nil), outputs...)}
}

func (s Shape) Clone() Shape { return NewShape(s.Inputs, s.Outputs) }

func (s Shape) Empty() bool { return len(s.Inputs) == 0 && len(s.Outputs) == 0 }

func (s Shape) Validate() error {
	if s.Empty() {
		return errors.New("port shape must contain an input or output")
	}
	seen := make(map[string]struct{}, len(s.Inputs)+len(s.Outputs))
	for _, ports := range [2][]Port{s.Inputs, s.Outputs} {
		for _, port := range ports {
			if port.id == "" {
				return errors.New("port id must not be empty")
			}
			if _, exists := seen[port.id]; exists {
				return fmt.Errorf("port id %q is repeated", port.id)
			}
			seen[port.id] = struct{}{}
			if port.schemaProblem != nil {
				return fmt.Errorf("port %q has an invalid schema: %w", port.id, port.schemaProblem)
			}
			if port.schema.IsZero() {
				return fmt.Errorf("port %q has an undefined schema", port.id)
			}
			if port.multiplicity < One || port.multiplicity > ManyMultiplicity {
				return fmt.Errorf("port %q has an invalid multiplicity", port.id)
			}
		}
	}
	for _, port := range s.Inputs {
		if port.direction != InputDirection {
			return fmt.Errorf("port %q is not an input", port.id)
		}
	}
	for _, port := range s.Outputs {
		if port.direction != OutputDirection {
			return fmt.Errorf("port %q is not an output", port.id)
		}
	}
	return nil
}

// Input is a value-type borrowed view. The schema trait supplies fork and
// drop operations. It deliberately carries no runtime claim/refcount state:
// conformance instrumentation, rather than the release hot path, detects
// double Take or use-after-Take in later milestones.
type Input[T any] struct {
	value T
	fork  func(T) T
	drop  func(T)
	valid bool
}

func NewInput[T any](value T, typ schema.Type[T]) Input[T] {
	if !typ.Valid() {
		return Input[T]{}
	}
	traits := typ.Traits()
	return NewInputWithTraits(value, traits.Fork, traits.Drop)
}

func NewInputWithTraits[T any](value T, fork func(T) T, drop func(T)) Input[T] {
	return Input[T]{value: value, fork: fork, drop: drop, valid: true}
}

func (i Input[T]) Valid() bool { return i.valid }

// Value is a borrow valid only during the current call. The owner must not be
// released while the borrow is in use.
func (i Input[T]) Value() T { return i.value }

// Take moves the item to an Owned value. The caller must not use or drop the
// Input after taking it.
func (i Input[T]) Take() Owned[T] {
	if !i.valid {
		return Owned[T]{}
	}
	return Owned[T]{value: i.value, fork: i.fork, drop: i.drop, valid: true}
}

// Share creates a retained value through the schema's Fork trait. The input
// remains borrowed and is not consumed.
func (i Input[T]) Share() Shared[T] {
	if !i.valid {
		return Shared[T]{}
	}
	value := i.value
	if i.fork != nil {
		value = i.fork(value)
	}
	return Shared[T]{value: value, fork: i.fork, drop: i.drop, valid: true}
}

func (i Input[T]) Drop() {
	if i.valid && i.drop != nil {
		i.drop(i.value)
	}
}

// Owned is the explicit owner returned by Input.Take. Release is a move
// operation and must be called once by its owner.
type Owned[T any] struct {
	value T
	fork  func(T) T
	drop  func(T)
	valid bool
}

func (o Owned[T]) Valid() bool { return o.valid }
func (o Owned[T]) Value() T    { return o.value }

func (o Owned[T]) Share() Shared[T] {
	if !o.valid {
		return Shared[T]{}
	}
	value := o.value
	if o.fork != nil {
		value = o.fork(value)
	}
	return Shared[T]{value: value, fork: o.fork, drop: o.drop, valid: true}
}

func (o Owned[T]) Release() {
	if o.valid && o.drop != nil {
		o.drop(o.value)
	}
}

func (o Owned[T]) Close() error {
	o.Release()
	return nil
}

// Shared is a retained value obtained from Input.Share or Owned.Share.
type Shared[T any] struct {
	value T
	fork  func(T) T
	drop  func(T)
	valid bool
}

func (s Shared[T]) Valid() bool { return s.valid }
func (s Shared[T]) Value() T    { return s.value }

func (s Shared[T]) Release() {
	if s.valid && s.drop != nil {
		s.drop(s.value)
	}
}

func (s Shared[T]) Close() error {
	s.Release()
	return nil
}

// Reader returns ownership to its consumer with each successful Read.
type Reader[T any] interface {
	Read(context.Context) (Input[T], error)
}

// Writer consumes an Input only after it has successfully accepted it. On an
// error the writer must leave the Input untouched, so the caller retains
// ownership.
type Writer[T any] interface {
	Write(context.Context, Input[T]) error
}

// Emitter follows the same move-on-success rule as Writer.
type Emitter[T any] interface {
	Emit(context.Context, Input[T]) error
}

// Processor is the common one-item transform contract. Flush handles delayed
// output without introducing a second runtime model.
type Processor[I, O any] interface {
	Process(context.Context, Input[I], Emitter[O]) error
	Flush(context.Context, Emitter[O]) error
}

// Operator is the erased lifecycle value returned by component Open. Typed
// Reader/Writer or Processor implementations are checked once by the caller
// at Open time; items never pass through any.
type Operator interface {
	Ports() Shape
	Close() error
}
