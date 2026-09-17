package mp3

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

type reader struct {
	shape      flow.Shape
	inspection inspection
	absolute   int64
	sequence   uint64
	out        flow.Item[packet.Chunk]
}

func newReader(plan readerPlan) *reader {
	return &reader{shape: plan.shape.Clone(), inspection: plan.inspection}
}

func (r *reader) Ports() flow.Shape { return r.shape.Clone() }
func (*reader) Close() error        { return nil }

func (r *reader) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[packet.Chunk]) error {
	if !input.Valid() {
		return errors.New("MP3 reader received unowned bytes")
	}
	defer input.Drop()
	data := input.Value().Bytes()
	if r.absolute < 0 || int64(data.Len()) > math.MaxInt64-r.absolute {
		return fmt.Errorf("%w: input offset overflows", ErrMalformed)
	}
	itemStart := r.absolute
	itemEnd := itemStart + int64(data.Len())
	r.absolute = itemEnd
	start := itemStart
	if start < r.inspection.audioStart {
		start = r.inspection.audioStart
	}
	end := itemEnd
	if end > r.inspection.audioEnd {
		end = r.inspection.audioEnd
	}
	if start >= end {
		return nil
	}
	localStart := int(start - itemStart)
	payload, err := input.Value().Range(localStart, int(end-start))
	if err != nil {
		return err
	}
	output.Own(&r.out, packet.NewChunk(r.sequence, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload))
	defer r.out.Drop()
	if err := output.Emit(ctx, &r.out); err != nil {
		return err
	}
	r.sequence++
	return nil
}

func (r *reader) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	if r.absolute != r.inspection.sourceEnd {
		if r.absolute < r.inspection.sourceEnd {
			return fmt.Errorf("%w: read through byte %d of %d", ErrTruncated, r.absolute, r.inspection.sourceEnd)
		}
		return fmt.Errorf("%w: input extends beyond source size", ErrMalformed)
	}
	return nil
}
