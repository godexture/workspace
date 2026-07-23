// Package framequeue holds output queues for filter engines whose output
// cadence does not match their input cadence one-to-one.
package framequeue

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// Multi is a FIFO of pending output frames, for filters that may emit
// zero, one, or many frames per SendFrame call (for example hop-based
// processing whose hop size does not line up with the caller's frame
// size). Unlike engine.Slot, Push never rejects a second pending item.
type Multi struct {
	pending []media.Frame
	flushed bool
}

func (q *Multi) Push(frame media.Frame) error {
	q.pending = append(q.pending, frame)
	return nil
}

func (q *Multi) Receive() (media.Frame, error) {
	if len(q.pending) == 0 {
		if q.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}
	frame := q.pending[0]
	q.pending = q.pending[1:]
	return frame, nil
}

func (q *Multi) Flush() {
	q.flushed = true
}

func (q *Multi) Close() {
	for _, frame := range q.pending {
		frame.Release()
	}
	q.pending = nil
}
