// Package flow defines typed ports and lifecycle/ownership contracts without
// exposing queues, goroutines, channels, or a scheduler.
package flow

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

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

func Required() PortOption {
	return func(options *portOptions) {
		options.required = true
		options.multiplicity = One
	}
}

func Optional() PortOption {
	return func(options *portOptions) {
		options.required = false
		options.multiplicity = OptionalMultiplicity
	}
}

func Many() PortOption {
	return func(options *portOptions) {
		options.required = false
		options.multiplicity = ManyMultiplicity
	}
}

func WithMultiplicity(value Multiplicity) PortOption {
	return func(options *portOptions) {
		options.multiplicity = value
		options.required = value == One
	}
}

// Port is an erased static port declaration. The schema identity is retained
// on the port; the typed constructors ensure it is created while T is known.
type Port struct {
	id           string
	direction    Direction
	schema       schema.ID
	required     bool
	multiplicity Multiplicity
}

func In[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, InputDirection, typ.Identity(), options...)
}

func Out[T any](id string, typ schema.Type[T], options ...PortOption) Port {
	return newPort(id, OutputDirection, typ.Identity(), options...)
}

func newPort(id string, direction Direction, identity schema.ID, options ...PortOption) Port {
	state := portOptions{required: true, multiplicity: One}
	for _, option := range options {
		if option != nil {
			option(&state)
		}
	}
	return Port{id: id, direction: direction, schema: identity, required: state.required, multiplicity: state.multiplicity}
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
			if port.schema.IsZero() {
				return fmt.Errorf("port %q has an undefined schema", port.id)
			}
			if port.multiplicity < One || port.multiplicity > ManyMultiplicity {
				return fmt.Errorf("port %q has an invalid multiplicity", port.id)
			}
			if port.multiplicity == OptionalMultiplicity && port.required {
				return fmt.Errorf("optional port %q cannot be required", port.id)
			}
			if port.multiplicity == ManyMultiplicity && port.required {
				return fmt.Errorf("many port %q cannot be required", port.id)
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

type ownership[T any] struct {
	refs    atomic.Int64
	claimed atomic.Bool
	value   T
	drop    func(T)
}

type handle[T any] struct {
	state    *ownership[T]
	released atomic.Bool
}

func newOwnership[T any](value T, drop func(T)) *ownership[T] {
	state := &ownership[T]{value: value, drop: drop}
	state.refs.Store(1)
	return state
}

func (state *ownership[T]) release() {
	if state == nil || state.refs.Add(-1) != 0 {
		return
	}
	if state.drop != nil {
		state.drop(state.value)
	}
}

func (state *ownership[T]) retain() bool {
	if state == nil || state.claimed.Load() {
		return false
	}
	for {
		refs := state.refs.Load()
		if refs == 0 {
			return false
		}
		if state.refs.CompareAndSwap(refs, refs+1) {
			if state.claimed.Load() {
				state.release()
				return false
			}
			return true
		}
	}
}

// Input is a borrowed view over an item whose ownership starts with the
// reader. Take transfers that ownership, Share creates a retained handle, and
// Drop releases it. Reader return transfers the item to its consumer; writer
// success transfers it to the writer, while writer failure leaves it with the
// caller.
type Input[T any] struct{ state *ownership[T] }

func NewInput[T any](value T, drop func(T)) Input[T] {
	return Input[T]{state: newOwnership(value, drop)}
}

func (i Input[T]) Valid() bool {
	return i.state != nil && i.state.refs.Load() > 0 && !i.state.claimed.Load()
}

func (i Input[T]) Value() T {
	if i.state == nil {
		var zero T
		return zero
	}
	return i.state.value
}

func (i Input[T]) Take() Owned[T] {
	if i.state == nil || !i.state.claimed.CompareAndSwap(false, true) {
		return Owned[T]{}
	}
	return Owned[T]{handle: &handle[T]{state: i.state}}
}

func (i Input[T]) Share() Shared[T] {
	if !i.state.retain() {
		return Shared[T]{}
	}
	return Shared[T]{handle: &handle[T]{state: i.state}}
}

func (i Input[T]) Drop() {
	if i.state != nil && i.state.claimed.CompareAndSwap(false, true) {
		i.state.release()
	}
}

// Owned is the explicit owner returned by Input.Take.
type Owned[T any] struct{ handle *handle[T] }

func (o Owned[T]) Valid() bool {
	return o.handle != nil && o.handle.state != nil && o.handle.state.refs.Load() > 0
}
func (o Owned[T]) Value() T {
	if o.handle == nil || o.handle.state == nil {
		var zero T
		return zero
	}
	return o.handle.state.value
}
func (o Owned[T]) Share() Shared[T] {
	if o.handle == nil || !o.handle.state.retain() {
		return Shared[T]{}
	}
	return Shared[T]{handle: &handle[T]{state: o.handle.state}}
}
func (o Owned[T]) Release() { releaseHandle(o.handle) }
func (o Owned[T]) Close() error {
	o.Release()
	return nil
}

// Shared is a retained item handle. Close/Release are idempotent per handle.
type Shared[T any] struct{ handle *handle[T] }

func (s Shared[T]) Valid() bool {
	return s.handle != nil && s.handle.state != nil && s.handle.state.refs.Load() > 0
}
func (s Shared[T]) Value() T {
	if s.handle == nil || s.handle.state == nil {
		var zero T
		return zero
	}
	return s.handle.state.value
}
func (s Shared[T]) Release() { releaseHandle(s.handle) }
func (s Shared[T]) Close() error {
	s.Release()
	return nil
}

func releaseHandle[T any](value *handle[T]) {
	if value != nil && value.released.CompareAndSwap(false, true) {
		value.state.release()
	}
}

// Reader returns ownership to its consumer with each successful Read.
type Reader[T any] interface {
	Read(context.Context) (Input[T], error)
}

// Writer consumes an Input only after it has successfully accepted it. On an
// error the writer must not call Take, so the caller retains ownership.
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
