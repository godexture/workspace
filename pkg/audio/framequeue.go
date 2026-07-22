package audio

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// FrameQueue holds at most one pending output frame, matching the
// node.Filter contract of emitting exactly one frame per ReceiveFrame call.
type FrameQueue struct {
	pending media.Frame
	flushed bool
}

func (q *FrameQueue) Push(frame media.Frame) error {
	if q.pending != nil {
		return fmt.Errorf("filter has an unconsumed output frame")
	}
	q.pending = frame
	return nil
}

func (q *FrameQueue) Receive() (*media.Frame, error) {
	if q.pending == nil {
		if q.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}
	frame := q.pending
	q.pending = nil
	return &frame, nil
}

func (q *FrameQueue) Flush() {
	q.flushed = true
}

func (q *FrameQueue) Close() {
	if q.pending != nil {
		q.pending.Release()
		q.pending = nil
	}
}
