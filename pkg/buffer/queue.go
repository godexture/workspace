package buffer

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// Queue is a FIFO of pending output items, for engines that may emit
// zero, one, or many items per Send call (for example hop-based
// processing whose hop size does not line up with the caller's frame
// size). Unlike Slot, Push never rejects a second pending item.
type Queue[T media.Retainer] struct {
	pending []T
	flushed bool
}

func (q *Queue[T]) Push(item T) error {
	q.pending = append(q.pending, item)
	return nil
}

func (q *Queue[T]) Receive() (T, error) {
	if len(q.pending) == 0 {
		var zero T
		if q.flushed {
			return zero, engine.ErrEOF
		}
		return zero, engine.ErrEAGAIN
	}
	item := q.pending[0]
	var zero T
	q.pending[0] = zero // clear reference to allow GC if backing array stays around
	q.pending = q.pending[1:]
	return item, nil
}

func (q *Queue[T]) Flush() {
	q.flushed = true
}

func (q *Queue[T]) Close() {
	for _, item := range q.pending {
		item.Release()
	}
	q.pending = nil
}
