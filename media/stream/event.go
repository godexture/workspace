package stream

import (
	"errors"

	"github.com/godexture/godec/media/property"
)

// EventKind identifies a live topology change.
type EventKind uint8

const (
	StreamAdded EventKind = iota + 1
	StreamRemoved
	StreamChanged
)

func (k EventKind) Valid() bool { return k >= StreamAdded && k <= StreamChanged }

// Decision is deliberately undecided until a consumer applies its policy.
type Decision uint8

const (
	Undecided Decision = iota
	Follow
	Ignore
	Fail
)

func (d Decision) Valid() bool { return d <= Fail }

var (
	ErrInvalidEvent      = errors.New("stream event is invalid")
	ErrInvalidDecision   = errors.New("stream event decision is invalid")
	ErrMissingDescriptor = errors.New("stream-added event needs a descriptor")
)

// Event carries one immutable live topology change. Added events carry a
// descriptor, while removed and changed events name the affected stream;
// changed events also carry the replacement property set.
type Event struct {
	kind       EventKind
	descriptor Descriptor
	id         ID
	properties property.Set
	decision   Decision
}

// NewAdded creates an event for a new stream.
func NewAdded(descriptor Descriptor) (Event, error) {
	if !descriptor.Valid() {
		return Event{}, ErrMissingDescriptor
	}
	return Event{kind: StreamAdded, descriptor: descriptor, id: descriptor.ID()}, nil
}

// NewRemoved creates an event for a removed stream.
func NewRemoved(id ID) (Event, error) {
	if id.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	return Event{kind: StreamRemoved, id: id}, nil
}

// NewChanged creates an event for a stream property replacement.
func NewChanged(id ID, properties property.Set) (Event, error) {
	if id.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	return Event{kind: StreamChanged, id: id, properties: properties}, nil
}

func (e Event) Valid() bool {
	if !e.kind.Valid() || !e.decision.Valid() {
		return false
	}
	switch e.kind {
	case StreamAdded:
		return e.descriptor.Valid() && !e.id.IsZero()
	case StreamRemoved, StreamChanged:
		return !e.id.IsZero()
	default:
		return false
	}
}

func (e Event) Kind() EventKind { return e.kind }

// Descriptor returns the descriptor for an added event.
func (e Event) Descriptor() (Descriptor, bool) {
	if e.kind != StreamAdded {
		return Descriptor{}, false
	}
	return e.descriptor, true
}

// ID names the affected stream. Two streams carrying the same schema are
// distinct here, which is what lets a removal or a property change name one of
// them.
func (e Event) ID() ID { return e.id }

// Properties returns the replacement properties for a changed event.
func (e Event) Properties() (property.Set, bool) {
	if e.kind != StreamChanged {
		return property.Set{}, false
	}
	return e.properties, true
}

func (e Event) Decision() Decision { return e.decision }

// WithDecision returns a copy. Undecided is a valid result and is never
// replaced with an implicit follow, ignore, or fail policy.
func (e Event) WithDecision(value Decision) (Event, error) {
	if !value.Valid() {
		return Event{}, ErrInvalidDecision
	}
	e.decision = value
	return e, nil
}
