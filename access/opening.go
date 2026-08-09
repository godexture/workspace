package access

import "errors"

var errInvalidOpening = errors.New("access opening is invalid")

// Direction is the direction of one node-local Access binding.
type Direction uint8

const (
	SourceDirection Direction = iota + 1
	SinkDirection
)

func (d Direction) Valid() bool { return d == SourceDirection || d == SinkDirection }

// Opening is the selected, private reference and capability view handed only
// to the component bound to that reference. It is not included in Plan.
type Opening struct {
	direction Direction
	reference Reference
	available Capabilities
	selected  []Capability
	class     TransactionClass
}

func NewOpening(direction Direction, reference Reference, available Capabilities, selected []Capability, class TransactionClass) (Opening, error) {
	if !direction.Valid() || !reference.Valid() || !available.Valid() || class != 0 && !class.Valid() {
		return Opening{}, errInvalidOpening
	}
	previous := Capability("")
	for index, capability := range selected {
		if !capability.Valid() || index != 0 && capability <= previous || !available.Contains(capability) {
			return Opening{}, ErrInvalidCapabilities
		}
		previous = capability
	}
	return Opening{
		direction: direction,
		reference: reference,
		available: Capabilities{values: available.Values()},
		selected:  append([]Capability(nil), selected...),
		class:     class,
	}, nil
}

func (o Opening) Valid() bool {
	if !o.direction.Valid() || !o.reference.Valid() || !o.available.Valid() || o.class != 0 && !o.class.Valid() {
		return false
	}
	previous := Capability("")
	for index, capability := range o.selected {
		if !capability.Valid() || index != 0 && capability <= previous || !o.available.Contains(capability) {
			return false
		}
		previous = capability
	}
	return true
}
func (o Opening) Direction() Direction               { return o.direction }
func (o Opening) Reference() Reference               { return o.reference }
func (o Opening) Available() Capabilities            { return Capabilities{values: o.available.Values()} }
func (o Opening) Selected() []Capability             { return append([]Capability(nil), o.selected...) }
func (o Opening) TransactionClass() TransactionClass { return o.class }
