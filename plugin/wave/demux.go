package wave

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

type demuxer struct {
	out         flow.Item[packet.Chunk]
	shape       flow.Shape
	buffers     *buffer.Allocator
	dataOffset  int64
	dataSize    uint64
	blockAlign  int
	absolute    int64
	consumed    uint64
	sequence    uint64
	sample      int64
	carry       [8]byte
	carryLength int
}

func newDemuxer(plan demuxPlan, buffers *buffer.Allocator) *demuxer {
	return &demuxer{
		shape:      plan.shape.Clone(),
		buffers:    buffers,
		dataOffset: plan.header.dataOffset,
		dataSize:   plan.header.dataSize,
		blockAlign: plan.header.blockAlign,
	}
}

func (d *demuxer) Ports() flow.Shape { return d.shape.Clone() }
func (*demuxer) Close() error        { return nil }

func (d *demuxer) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[packet.Chunk]) error {
	if !input.Valid() {
		return errors.New("WAVE demuxer received unowned bytes")
	}
	defer input.Drop()
	data := input.Value().Bytes()
	if d.absolute < 0 || int64(len(data)) > math.MaxInt64-d.absolute {
		return fmt.Errorf("%w: input offset overflows", ErrMalformed)
	}
	itemStart := d.absolute
	itemEnd := itemStart + int64(len(data))
	d.absolute = itemEnd
	dataEnd := d.dataOffset + int64(d.dataSize)
	start := itemStart
	if start < d.dataOffset {
		start = d.dataOffset
	}
	end := itemEnd
	if end > dataEnd {
		end = dataEnd
	}
	if start >= end {
		return nil
	}
	localStart := int(start - itemStart)
	segment := data[localStart:int(end-itemStart)]
	d.consumed += uint64(len(segment))
	if d.consumed > d.dataSize {
		return fmt.Errorf("%w: data range exceeded its declared size", ErrMalformed)
	}

	if d.carryLength != 0 {
		needed := d.blockAlign - d.carryLength
		if len(segment) < needed {
			copy(d.carry[d.carryLength:], segment)
			d.carryLength += len(segment)
			return nil
		}
		copy(d.carry[d.carryLength:], segment[:needed])
		if err := d.emitCopy(ctx, d.carry[:d.blockAlign], output); err != nil {
			return err
		}
		d.carryLength = 0
		localStart += needed
		segment = segment[needed:]
	}

	aligned := len(segment) - len(segment)%d.blockAlign
	if aligned != 0 {
		payload, err := input.Value().Range(localStart, aligned)
		if err != nil {
			return err
		}
		if err := d.emit(ctx, payload, output); err != nil {
			return err
		}
		localStart += aligned
		segment = segment[aligned:]
	}
	if len(segment) != 0 {
		copy(d.carry[:], segment)
		d.carryLength = len(segment)
	}
	if d.consumed == d.dataSize && d.carryLength != 0 {
		return ErrPartialBlock
	}
	return nil
}

func (d *demuxer) Flush(context.Context, flow.Emitter[packet.Chunk]) error {
	if d.consumed != d.dataSize {
		return fmt.Errorf("%w: read %d of %d bytes", ErrTruncatedData, d.consumed, d.dataSize)
	}
	if d.carryLength != 0 {
		return ErrPartialBlock
	}
	return nil
}

func (d *demuxer) emitCopy(ctx context.Context, value []byte, output flow.Emitter[packet.Chunk]) error {
	lease, err := d.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: len(value)}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		copy(storage.Bytes(), value)
		return nil
	}); err != nil {
		return err
	}
	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	return d.emit(ctx, payload, output)
}

func (d *demuxer) emit(ctx context.Context, payload buffer.Handle, output flow.Emitter[packet.Chunk]) error {
	frames := len(payload.Bytes()) / d.blockAlign
	d.out.Set(packet.NewChunk(d.sequence, timing.SomePTS(timing.NewPTS(d.sample)), payload), mediaformat.Chunks())
	defer d.out.Drop()
	if err := output.Emit(ctx, &d.out); err != nil {
		return err
	}
	d.sequence++
	d.sample += int64(frames)
	return nil
}
