package buffer

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// Slot holds at most one pending output item, matching the convention (used
// by Filter, Encoder, and Decoder engines alike) of emitting exactly one item
// per Receive call: Push rejects a second item until the first has been
// drained, and Receive distinguishes "nothing yet" (engine.ErrEAGAIN) from "nothing
// left" (engine.ErrEOF, once Flush has been called) so callers can tell a stall from
// a legitimate end of stream.
type Slot[T media.Retainer] struct {
	pending T
	has     bool
	flushed bool
}

func (s *Slot[T]) Push(item T) error {
	if s.has {
		return fmt.Errorf("output slot already has an unconsumed item")
	}
	s.pending = item
	s.has = true
	return nil
}

func (s *Slot[T]) Receive() (T, error) {
	if !s.has {
		var zero T
		if s.flushed {
			return zero, engine.ErrEOF
		}
		return zero, engine.ErrEAGAIN
	}
	item := s.pending
	var zero T
	s.pending = zero
	s.has = false
	return item, nil
}

func (s *Slot[T]) Flush() {
	s.flushed = true
}

func (s *Slot[T]) Close() {
	if s.has {
		s.pending.Release()
		var zero T
		s.pending = zero
		s.has = false
	}
}
