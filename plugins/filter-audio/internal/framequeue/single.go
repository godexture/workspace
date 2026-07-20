package framequeue

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

type Single struct {
	pending media.Frame
	flushed bool
}

func (q *Single) Push(frame media.Frame) error {
	if q.pending != nil {
		return fmt.Errorf("filter has an unconsumed output frame")
	}
	q.pending = frame
	return nil
}

func (q *Single) Receive() (*media.Frame, error) {
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

func (q *Single) Flush() {
	q.flushed = true
}

func (q *Single) Close() {
	if q.pending != nil {
		q.pending.Release()
		q.pending = nil
	}
}
