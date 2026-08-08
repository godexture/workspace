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

// Batch is a call-scoped borrowed view of one deterministic fan-in group. It
// exposes no consume operation; runtime retains every owner until Process
// returns and then applies the success/error ownership rule as a unit.
type Batch[T any] struct{ inputs []Input[T] }

func NewBatch[T any](inputs []Input[T]) Batch[T] { return Batch[T]{inputs: inputs} }
func (b Batch[T]) Len() int                      { return len(b.inputs) }

func (b Batch[T]) Value(index int) (T, bool) {
	if index < 0 || index >= len(b.inputs) || !b.inputs[index].Valid() {
		var zero T
		return zero, false
	}
	return b.inputs[index].Value(), true
}

func (b Batch[T]) Share(index int) (Shared[T], bool) {
	if index < 0 || index >= len(b.inputs) || !b.inputs[index].Valid() {
		return Shared[T]{}, false
	}
	return b.inputs[index].Share(), true
}

// Processor is the common one-item transform contract. Process consumes its
// input only when it succeeds; on error the caller retains ownership. Flush
// handles delayed output without introducing a second runtime model.
type Processor[I, O any] interface {
	Process(context.Context, Input[I], Emitter[O]) error
	Flush(context.Context, Emitter[O]) error
}

// Joiner transforms deterministic groups from a homogeneous many-input port.
// Inputs are borrowed for the call; runtime consumes the group on success and
// retains then drops it after a failed call.
type Joiner[I, O any] interface {
	Process(context.Context, Batch[I], Emitter[O]) error
	Flush(context.Context, Emitter[O]) error
}

// Operator is the erased lifecycle value returned by component Open. Typed
// Reader/Writer or Processor implementations are checked once by the caller
// at Open time; items never pass through any.
type Operator interface {
	Ports() Shape
	Close() error
}
