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
	ManyMultiplicity
)

// FanInPolicy defines the semantic ordering of a many-input port. A policy is
// mandatory for many input ports and meaningless on outputs or one-input
// ports.
type FanInPolicy uint8

const (
	ZipFanIn FanInPolicy = iota + 1
	LatestFanIn
	PrimaryFanIn
	MergeFanIn
	WindowFanIn
)

func (p FanInPolicy) Valid() bool { return p >= ZipFanIn && p <= WindowFanIn }

type PortOption func(*portOptions)

type portOptions struct {
	required     bool
	multiplicity Multiplicity
	fanIn        FanInPolicy
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

// WithFanIn assigns the semantic policy for a Many input port.
func WithFanIn(policy FanInPolicy) PortOption {
	return func(options *portOptions) { options.fanIn = policy }
}

// Port is an erased static port declaration. The schema descriptor retains
// marker identity and payload type; typed runtime operations remain on the
// schema.Type captured by execution binding constructors.
type Port struct {
	id           string
	direction    Direction
	descriptor   schema.Descriptor
	required     bool
	multiplicity Multiplicity
	fanIn        FanInPolicy
}

func In[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, InputDirection, typ.Descriptor(), options...)
}

func Out[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, OutputDirection, typ.Descriptor(), options...)
}

func newPort(id string, direction Direction, descriptor schema.Descriptor, options ...PortOption) Port {
	state := portOptions{required: true, multiplicity: One}
	for _, option := range options {
		if option != nil {
			option(&state)
		}
	}
	return Port{id: id, direction: direction, descriptor: descriptor, required: state.required, multiplicity: state.multiplicity, fanIn: state.fanIn}
}

func (p Port) ID() string                 { return p.id }
func (p Port) Direction() Direction       { return p.direction }
func (p Port) Schema() schema.Descriptor  { return p.descriptor }
func (p Port) Required() bool             { return p.required }
func (p Port) Multiplicity() Multiplicity { return p.multiplicity }
func (p Port) FanIn() FanInPolicy         { return p.fanIn }

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

// Equal reports semantic equality. Identity and payload type together
// determine whether typed edges can be wired safely.
func (s Shape) Equal(other Shape) bool {
	return equalPorts(s.Inputs, other.Inputs) && equalPorts(s.Outputs, other.Outputs)
}

func equalPorts(left, right []Port) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].id != right[index].id ||
			left[index].direction != right[index].direction ||
			left[index].descriptor.Identity() != right[index].descriptor.Identity() ||
			left[index].descriptor.Payload() != right[index].descriptor.Payload() ||
			left[index].required != right[index].required ||
			left[index].multiplicity != right[index].multiplicity ||
			left[index].fanIn != right[index].fanIn {
			return false
		}
	}
	return true
}

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
			if problem := port.descriptor.Problem(); problem != nil {
				return fmt.Errorf("port %q has an invalid schema: %w", port.id, problem)
			}
			if !port.descriptor.Valid() {
				return fmt.Errorf("port %q has an undefined schema", port.id)
			}
			if port.multiplicity < One || port.multiplicity > ManyMultiplicity {
				return fmt.Errorf("port %q has an invalid multiplicity", port.id)
			}
			if port.direction == InputDirection && port.multiplicity == ManyMultiplicity {
				if !port.fanIn.Valid() {
					return fmt.Errorf("many input port %q requires a fan-in policy", port.id)
				}
			} else if port.fanIn != 0 {
				return fmt.Errorf("port %q has a fan-in policy without many-input multiplicity", port.id)
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

// Reader fills the caller's cell with the next item. It reports io.EOF when
// the stream is complete and leaves the cell empty.
type Reader[T any] interface {
	Read(context.Context, *Item[T]) error
}

// Writer accepts one item. Consuming the cell claims ownership; leaving it
// alone returns the item to whoever passed the pointer.
type Writer[T any] interface {
	Write(context.Context, *Item[T]) error
}

// Emitter follows the same rule as Writer.
type Emitter[T any] interface {
	Emit(context.Context, *Item[T]) error
}

// Batch is one deterministic fan-in group. Each cell follows the ordinary
// ownership rule, so a Joiner may consume some, all, or none of them.
type Batch[T any] struct{ items []*Item[T] }

func NewBatch[T any](items []*Item[T]) Batch[T] { return Batch[T]{items: items} }

func (b Batch[T]) Len() int { return len(b.items) }

// At returns the cell at index, or nil when the index is out of range.
func (b Batch[T]) At(index int) *Item[T] {
	if index < 0 || index >= len(b.items) {
		return nil
	}
	return b.items[index]
}

func (b Batch[T]) Value(index int) (T, bool) {
	item := b.At(index)
	if !item.Valid() {
		var zero T
		return zero, false
	}
	return item.Value(), true
}

// Processor is the common one-item transform contract. Flush handles delayed
// output without introducing a second runtime model.
type Processor[I, O any] interface {
	Process(context.Context, *Item[I], Emitter[O]) error
	Flush(context.Context, Emitter[O]) error
}

// Joiner transforms deterministic groups from a homogeneous many-input port.
type Joiner[I, O any] interface {
	Process(context.Context, Batch[I], Emitter[O]) error
	Flush(context.Context, Emitter[O]) error
}

// Finalizer prepares successful end-of-stream state before Processor Flush is
// propagated. It is never called during failure or cancellation cleanup.
type Finalizer interface {
	Finalize(context.Context) error
}

// Operator is the erased lifecycle value returned by component Open. Typed
// Reader/Writer or Processor implementations are checked once by the caller
// at Open time; items never pass through any.
type Operator interface {
	Ports() Shape
	Close() error
}
