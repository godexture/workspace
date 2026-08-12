package access

import "errors"

var (
	ErrInvalidOpening  = errors.New("access opening is invalid")
	ErrTransactionView = errors.New("access sink session does not provide its transaction contract")
)

// Direction is the direction of one node-local Access binding.
type Direction uint8

const (
	SourceDirection Direction = iota + 1
	SinkDirection
)

func (d Direction) Valid() bool { return d == SourceDirection || d == SinkDirection }

// Opening contains only the operation views selected for one acquired Access
// session. The underlying session and reference are never exposed to the
// component.
type Opening struct {
	direction   Direction
	selected    []Capability
	class       TransactionClass
	views       viewSet
	transaction Transaction
	flusher     Flusher
	syncer      Syncer
}

func NewOpening(direction Direction, session Session, selection Selection, class TransactionClass) (Opening, error) {
	if !direction.Valid() || session == nil || !selection.ValidFor(direction) || direction == SourceDirection && class != 0 || direction == SinkDirection && !class.Valid() {
		return Opening{}, ErrInvalidOpening
	}
	views, err := viewsFor(session, selection)
	if err != nil {
		return Opening{}, err
	}
	result := Opening{
		direction: direction,
		selected:  selection.Capabilities(),
		class:     class,
		views:     views,
	}
	if direction == SinkDirection {
		result.transaction, _ = session.(Transaction)
		result.flusher, _ = session.(Flusher)
		result.syncer, _ = session.(Syncer)
		if class != LiveNoCommit && result.transaction == nil {
			return Opening{}, ErrTransactionView
		}
	}
	return result, nil
}

func (o Opening) Valid() bool {
	selection := Selection{capabilities: append([]Capability(nil), o.selected...)}
	if !o.direction.Valid() || !selection.ValidFor(o.direction) || o.direction == SourceDirection && o.class != 0 || o.direction == SinkDirection && !o.class.Valid() {
		return false
	}
	for _, capability := range o.selected {
		switch capability {
		case SequentialRead:
			if o.views.sequential == nil {
				return false
			}
		case RandomRead:
			if o.views.random == nil {
				return false
			}
		case StableSize:
			if o.views.sizer == nil {
				return false
			}
		case SequentialWrite:
			if o.views.appender == nil {
				return false
			}
		case RandomWrite:
			if o.views.patcher == nil {
				return false
			}
		}
	}
	return o.direction != SinkDirection || o.class == LiveNoCommit || o.transaction != nil
}

func (o Opening) Direction() Direction               { return o.direction }
func (o Opening) Selected() []Capability             { return append([]Capability(nil), o.selected...) }
func (o Opening) TransactionClass() TransactionClass { return o.class }

func SequentialOf(opening Opening) (Sequential, bool) {
	return opening.views.sequential, opening.Valid() && opening.views.sequential != nil
}

func RandomOf(opening Opening) (Random, bool) {
	return opening.views.random, opening.Valid() && opening.views.random != nil
}

// StableSizeOf returns the context-aware size view selected for a stable
// finite source. Growing and live sources do not select this capability.
func StableSizeOf(opening Opening) (Sizer, bool) {
	return opening.views.sizer, opening.Valid() && opening.views.sizer != nil
}

func AppenderOf(opening Opening) (Appender, bool) {
	return opening.views.appender, opening.Valid() && opening.views.appender != nil
}

func PatcherOf(opening Opening) (Patcher, bool) {
	return opening.views.patcher, opening.Valid() && opening.views.patcher != nil
}

func TransactionOf(opening Opening) (Transaction, bool) {
	return opening.transaction, opening.Valid() && opening.transaction != nil
}

func FlusherOf(opening Opening) (Flusher, bool) {
	return opening.flusher, opening.Valid() && opening.flusher != nil
}

func SyncerOf(opening Opening) (Syncer, bool) {
	return opening.syncer, opening.Valid() && opening.syncer != nil
}
