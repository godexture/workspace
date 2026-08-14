package flow

import (
	"errors"
	"runtime/debug"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/schema"
)

// ErrTransfer reports a linear hand-off whose source, target, or conversion is
// missing.
var ErrTransfer = errors.New("flow item transfer is invalid")

// Reporter is a failure domain: where a slot reports a release it could not
// perform. Releasing happens in deferred cleanup, where there is no return
// value left to carry a failure and where a panic would replace the failure
// that actually stopped the work, so the slot reports instead of returning.
type Reporter interface {
	Cleanup(error)
}

// ReleaseError reports a declared Drop that did not complete.
//
// It keeps the stack the release panicked from and never the value it panicked
// with: that value is chosen by the code that panicked and can be the data it
// was handling.
type ReleaseError struct {
	Summary string
	Stack   []byte
}

func (e *ReleaseError) Error() string {
	return "flow item release panicked: " + e.Summary
}

func (e *ReleaseError) StackTrace() []byte { return append([]byte(nil), e.Stack...) }

// Collector is a failure domain a caller holds itself. A test or a tool that
// owns slots outside a runtime task uses one, and reads back what releasing
// could not do.
type Collector struct{ failures []error }

func (c *Collector) Cleanup(err error) {
	if c != nil && err != nil {
		c.failures = append(c.failures, err)
	}
}

// Failures returns what this domain was told, oldest first.
func (c *Collector) Failures() []error {
	if c == nil {
		return nil
	}
	return append([]error(nil), c.failures...)
}

// NewItem fills a slot in the named domain. A runtime hands its stages slots it
// has already bound and they take ownership through Emitter.Own; this is for a
// caller that owns the domain itself, and the domain is required for the same
// reason it is there: a payload with no domain has nowhere to report a release
// it could not perform.
func NewItem[T any](value T, typ schema.Type[T], into Reporter) Item[T] {
	if !typ.Valid() || into == nil {
		typ.Drop(value)
		return Item[T]{}
	}
	traits := typ.Traits()
	return Item[T]{value: value, fork: traits.Fork, drop: traits.Drop, reporter: into, bound: true, valid: true}
}

// noCopy makes `go vet` reject copies of a value that carries a release
// obligation. Constructing and returning a cell stays legal; aliasing one,
// appending it to a container, ranging over it by value, or sending it across
// a channel does not.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Item is one ownership slot.
//
// A slot is not a box for a payload: it is a declared place to own one. Bind
// gives it the traits its payloads are owned under and the failure domain a
// failed release is reported to, and those stay with the slot. Ownership then
// moves between slots and never carries a domain with it, so a payload is
// always released, and always reported, where it currently lives.
//
// An Item is always passed as a pointer, and the first Drop or Move releases or
// removes its payload while every later one does nothing. That single rule
// replaces per-call ownership protocols. A stage receives a slot, defers Drop,
// and passes the pointer on; whoever consumes it wins, and if nobody does, the
// deferred Drop releases it. Success and failure need no distinction, because a
// consumed slot is already empty when the deferred Drop runs.
//
// Fork is the only way to obtain a second owner of the same logical payload, so
// fan-out is the only place that retains.
type Item[T any] struct {
	_        noCopy
	value    T
	fork     func(T) T
	drop     func(T)
	reporter Reporter
	bound    bool
	valid    bool
}

// Bind declares an empty slot: the type its payloads are owned under, and the
// failure domain that hears about a release it cannot perform.
//
// A slot that already holds a payload keeps what it was filled under, because
// that payload's release obligation was taken on in the domain that owns it
// now. Binding is how a runtime hands out slots; components receive theirs
// already bound and take ownership through Emitter.Own.
// A nil reporter leaves the domain the slot already belongs to, which is how a
// harness substitutes accounted traits into a slot the runtime handed it: a
// binding declares a domain, and never takes one away. It cannot establish a
// slot without one, because a payload owned outside every domain has nowhere to
// report a release it could not perform.
func (i *Item[T]) Bind(typ schema.Type[T], reporter Reporter) {
	if i == nil || i.valid || !typ.Valid() || (reporter == nil && !i.bound) {
		return
	}
	traits := typ.Traits()
	i.fork, i.drop, i.bound = traits.Fork, traits.Drop, true
	if reporter != nil {
		i.reporter = reporter
	}
}

// Set takes ownership of value under the slot's declared traits, releasing
// anything the slot still held. It either stores value or releases it on every
// path, so a caller hands a payload to Set and is done with it.
//
// An unbound slot declares nothing, so it cannot take ownership of anything:
// it has no release to perform and nowhere to report one that fails, and
// storing the payload anyway would lose it silently. Set leaves such a slot
// empty, which every runtime entry point rejects at once.
//
// A stage that emits many payloads keeps one slot and reuses it, which costs no
// allocation because the slot never escapes per item.
func (i *Item[T]) Set(value T) {
	if i == nil || !i.bound {
		return
	}
	i.Drop()
	i.value, i.valid = value, true
}

// Valid reports whether the slot still holds an unreleased payload.
func (i *Item[T]) Valid() bool { return i != nil && i.valid }

// Value borrows the held payload for the duration of the current call. It stays
// valid only until the slot is consumed.
func (i *Item[T]) Value() T {
	if i == nil {
		var zero T
		return zero
	}
	return i.value
}

// Drop releases the held payload. It is safe to defer immediately after
// receiving or filling a slot: once something else has consumed the slot, Drop
// does nothing.
//
// It never panics and never returns. A declared Drop is third-party code, and
// this call is the last thing a stage does on its way out -- often while a
// panic is already unwinding -- so a release that fails is reported to the
// slot's domain rather than raised over the failure that stopped the work.
func (i *Item[T]) Drop() {
	if i == nil || !i.valid {
		return
	}
	value := i.value
	i.empty()
	i.release(value)
}

// Move transfers ownership from source into i, leaving source empty. Anything i
// still held is released first. Neither slot's binding moves: the payload is
// now owned, and will be released, in i's domain.
func (i *Item[T]) Move(source *Item[T]) {
	if i == nil || source == nil || i == source {
		return
	}
	i.Drop()
	if !source.valid {
		return
	}
	i.inherit(source)
	value := source.value
	source.empty()
	i.value, i.valid = value, true
}

// inherit lets an undeclared slot take the sending slot's declaration. A
// receiving end that declares nothing of its own belongs to the domain the
// payload is already in; without this it would hold a payload it does not know
// how to release.
func (i *Item[T]) inherit(source *Item[T]) {
	if i.bound {
		return
	}
	i.fork, i.drop, i.reporter, i.bound = source.fork, source.drop, source.reporter, true
}

// Fork places an independent owner of the same logical payload into target. The
// receiver keeps its own ownership, so only genuine fan-out retains.
func (i *Item[T]) Fork(target *Item[T]) bool {
	if i == nil || target == nil || !i.valid {
		return false
	}
	value := i.value
	if i.fork != nil {
		value = i.fork(value)
	}
	target.inherit(i)
	target.Set(value)
	return true
}

// release runs the declared Drop away from the caller. The payload has already
// left the slot, so a failure here is nobody's to fix and everybody's to know
// about: it goes to the domain the slot belongs to.
func (i *Item[T]) release(value T) {
	if i.drop == nil {
		return
	}
	failure := invokeDrop(i.drop, value)
	if failure == nil || i.reporter == nil {
		return
	}
	i.reporter.Cleanup(failure)
}

func invokeDrop[T any](drop func(T), value T) (failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = &ReleaseError{
				Summary: diagnostic.Recovered(recovered),
				Stack:   append([]byte(nil), debug.Stack()...),
			}
		}
	}()
	drop(value)
	return nil
}

// empty removes the payload and keeps the binding: the slot stays the same
// declared place to own a payload of the same type in the same domain.
func (i *Item[T]) empty() {
	var zero T
	i.value, i.valid = zero, false
}

// Transfer moves ownership from source into target, rewrapping the payload for
// a different slot type. It is the linear hand-off: source is emptied without
// releasing, so the payload is never retained and no second owner exists at any
// point.
//
// build must carry source's payload into its result, which is what makes the
// unreleased source correct. Detaching the payload is therefore the last thing
// build should do, because a failure afterwards strands it. Use Fork instead
// when both slots must stay alive.
//
// into is the edge the rewrapped payload is headed for, and the domain target
// belongs to once it holds one.
func Transfer[I, O any](source *Item[I], target *Item[O], into Emitter[O], build func(I) (O, error)) error {
	if source == nil || target == nil || into == nil || !source.valid || build == nil {
		return ErrTransfer
	}
	value := source.value
	source.empty()
	// The source slot is empty from here, so its owner cannot release the
	// payload any more. Until build returns, this is the only obligation left;
	// once it returns, the payload lives in result and Set takes over, because
	// Set either stores what it is given or releases it.
	built := false
	defer func() {
		if !built {
			source.release(value)
		}
	}()
	result, err := build(value)
	if err != nil {
		return err
	}
	built = true
	into.Own(target, result)
	return nil
}
