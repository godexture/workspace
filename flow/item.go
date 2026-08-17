package flow

import (
	"errors"
	"runtime/debug"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/ownership"
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

// Owner is a failure domain whose lifetime covers a component's whole
// lifecycle, rather than one call or one lifecycle step.
//
// Slots a component fills through Emitter.Own are already owned for that long,
// so a component only needs this for a slot it fills some other way: one it
// Moves an input into and keeps across calls, releasing it during Flush or
// Close. Binding such a slot to the sender's domain instead would tie its
// lifetime to whichever caller happened to hand over the payload, which is why
// an unbound slot refuses ownership rather than inheriting one.
//
// The runtime supplies it through plugin.OpenContext; a component never
// constructs one.
type Owner interface{ Reporter }

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
	if into == nil {
		panic("flow: an item needs the failure domain it belongs to")
	}
	if !typ.Valid() {
		// The schema cannot describe ownership, so no slot can hold this
		// payload. Releasing it is still attempted, and a release that fails
		// is reported rather than raised: this runs wherever the caller was.
		report(into, invokeDrop(typ.Drop, value))
		return Item[T]{}
	}
	traits := typ.Traits()
	audited := hasFlowOwnership(into)
	if audited {
		trackFlowOwnership(into, 1)
	}
	return Item[T]{value: value, fork: traits.Fork, drop: traits.Drop, reporter: into, bound: true, valid: true, audited: audited}
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
	audited  bool
}

// Bind declares an empty slot: the type its payloads are owned under, and the
// failure domain that hears about a release it cannot perform.
//
// Binding succeeds once. The declaration remains with the slot while it is
// empty or holds a payload, so repeated Emitter.Own calls cannot change the
// release traits or failure domain of a slot that is being reused.
func (i *Item[T]) Bind(typ schema.Type[T], reporter Reporter) {
	if i == nil || i.bound || !typ.Valid() || reporter == nil {
		return
	}
	traits := typ.Traits()
	i.fork, i.drop, i.bound = traits.Fork, traits.Drop, true
	i.reporter = reporter
	i.audited = hasFlowOwnership(reporter)
}

// Set takes ownership of value under the slot's declared traits, releasing
// anything the slot still held. It either stores value or releases it on every
// path, so a caller hands a payload to Set and is done with it.
//
// A slot that is absent or unbound declares nothing, so it cannot take
// ownership: it knows no release to perform and no domain to report one that
// fails to. Handing a payload to one is a programming error and panics, because
// the alternatives
// are worse. Storing it would leave a payload nobody can release; returning
// quietly would lose it with no diagnosis at all, and an emptiness discovered
// later does not give back the release that was owed. A runtime hands out bound
// slots and Emitter.Own binds before it takes, so this cannot be reached by
// following the contract.
//
// A stage that emits many payloads keeps one slot and reuses it, which costs no
// allocation because the slot never escapes per item.
func (i *Item[T]) Set(value T) {
	if i == nil {
		panic("flow: ownership was handed to a slot that does not exist")
	}
	if !i.bound {
		panic("flow: an unbound slot cannot take ownership; fill it through Emitter.Own or a slot the runtime bound")
	}
	i.Drop()
	i.value, i.valid = value, true
	i.trackOwnership(1)
}

// Valid reports whether the slot still holds an unreleased payload.
func (i *Item[T]) Valid() bool { return i != nil && i.valid }

// Bound reports whether the slot declares a release and a domain, and so can
// take ownership at all. A boundary that is about to hand a payload over asks
// this first, so a slot that cannot take one is refused before the payload
// leaves where it currently lives.
func (i *Item[T]) Bound() bool { return i != nil && i.bound }

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
	i.trackOwnership(-1)
	i.release(value)
}

// Move transfers ownership from source into i, leaving source empty, and
// reports whether it did. Anything i still held is released first. Neither
// slot's binding moves: the payload is now owned, and will be released, in i's
// domain.
//
// An unbound target is refused, and source keeps its payload. A slot that
// declares nothing knows no release to perform and no domain to report one
// that fails to, and silently adopting the sender's declaration would decide
// the payload's lifetime by accident: a component keeping a value past the
// call it arrived in would be reporting into whichever caller's domain
// happened to hand it over. A slot that outlives its caller declares that
// itself, by binding to the Owner the runtime granted it.
func (i *Item[T]) Move(source *Item[T]) bool {
	if i == nil || source == nil || i == source || !i.bound || !source.valid {
		return false
	}
	i.Drop()
	value := source.value
	source.empty()
	source.trackOwnership(-1)
	i.value, i.valid = value, true
	i.trackOwnership(1)
	return true
}

// Fork places an independent owner of the same logical payload into target. The
// receiver keeps its own ownership, so only genuine fan-out retains.
//
// An unbound target is refused for the same reason Move refuses one, and
// nothing is forked: the receiver's payload stays exactly as it was.
func (i *Item[T]) Fork(target *Item[T]) bool {
	if i == nil || target == nil || i == target || !i.valid || !target.bound {
		return false
	}
	value := i.value
	if i.fork != nil {
		value = i.fork(value)
	}
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
	report(i.reporter, invokeDrop(i.drop, value))
}

// report hands a failure to a domain without letting it raise. A domain is
// third-party code as much as a declared Drop is, and this is the last step of
// a release: there is no return value left to carry a failure here, and a panic
// would replace whatever stopped the work. A domain that cannot even accept a
// report leaves no other trace.
func report(into Reporter, failure error) {
	if into == nil || failure == nil {
		return
	}
	defer func() { _ = recover() }()
	into.Cleanup(failure)
}

func hasFlowOwnership(into Reporter) bool {
	return ownership.Enabled(into)
}

func (i *Item[T]) trackOwnership(delta int64) {
	if i == nil || !i.audited {
		return
	}
	trackFlowOwnership(i.reporter, delta)
}

func trackFlowOwnership(into Reporter, delta int64) {
	ownership.Track(into, delta)
}

func invokeDrop[T any](drop func(T), value T) (failure error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			failure = errorx.MarkPanic(&ReleaseError{
				Summary: diagnostic.Recovered(recovered),
				Stack:   append([]byte(nil), stack...),
			}, stack)
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
	source.trackOwnership(-1)
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
