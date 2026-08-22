package mp4

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type demuxer struct {
	shape   flow.Shape
	tracks  []demuxTrack
	reader  access.Random
	buffers *buffer.Allocator
	items   []flow.Item[packet.Packet]
	cursor  sampleCursor
	track   int
	ready   bool
	done    bool
	failure error
}

func openDemuxer(ctx plugin.OpenContext, plan demuxPlan) (*demuxer, error) {
	if !validDemuxPlan(plan) {
		return nil, fmt.Errorf("%w: MP4 demux plan is invalid", ErrMalformed)
	}
	if ctx.Buffers() == nil {
		return nil, fmt.Errorf("%w: MP4 demuxer requires a payload buffer grant", ErrUnsupported)
	}
	opening, ok := mediaformat.SourceOpening(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: MP4 demuxer requires its inspected source opening", ErrUnsupported)
	}
	reader, ok := access.RandomOf(opening)
	if !ok {
		return nil, fmt.Errorf("%w: MP4 source opening lacks random read", ErrUnsupported)
	}
	sizer, ok := access.StableSizeOf(opening)
	if !ok {
		return nil, fmt.Errorf("%w: MP4 source opening lacks a stable size", ErrUnsupported)
	}
	size, err := sizer.Size(ctx.Context())
	if err != nil {
		return nil, err
	}
	if size < 0 || uint64(size) != plan.sourceEnd {
		return nil, fmt.Errorf("%w: MP4 source size changed after inspection", ErrMalformed)
	}
	return &demuxer{
		shape:   plan.shape.Clone(),
		tracks:  plan.tracks,
		reader:  reader,
		buffers: ctx.Buffers(),
		items:   make([]flow.Item[packet.Packet], len(plan.tracks)),
	}, nil
}

func validDemuxPlan(plan demuxPlan) bool {
	if !plan.shape.Equal(demuxerShape()) || plan.sourceEnd == 0 || len(plan.tracks) == 0 {
		return false
	}
	previous := -1
	for _, selected := range plan.tracks {
		if selected.inspectionIndex <= previous || validateDemuxTrack(selected.value) != nil {
			return false
		}
		previous = selected.inspectionIndex
	}
	return true
}

func (d *demuxer) Ports() flow.Shape { return d.shape.Clone() }
func (d *demuxer) Close() error {
	d.reader = nil
	d.buffers = nil
	d.tracks = nil
	d.items = nil
	d.cursor = sampleCursor{}
	return nil
}

func (d *demuxer) Read(ctx context.Context, output flow.RoutedEmitter[packet.Packet]) error {
	if d.failure != nil {
		return d.failure
	}
	if d.done {
		return io.EOF
	}
	if err := context.Cause(ctx); err != nil {
		d.failure = err
		return err
	}
	for d.track < len(d.tracks) {
		if !d.ready {
			cursor, err := newSampleCursor(ctx, d.reader, d.tracks[d.track].value)
			if err != nil {
				d.failure = err
				return err
			}
			d.cursor = cursor
			d.ready = true
		}
		value, more, err := d.cursor.next(ctx)
		if err != nil {
			d.failure = err
			return err
		}
		if !more {
			d.cursor = sampleCursor{}
			d.track++
			d.ready = false
			continue
		}
		if err := d.emit(ctx, d.track, value, output); err != nil {
			d.failure = err
			return err
		}
		return nil
	}
	d.done = true
	return io.EOF
}

func (d *demuxer) emit(ctx context.Context, ordinal int, value sample, output flow.RoutedEmitter[packet.Packet]) error {
	if ordinal < 0 || ordinal >= len(d.items) {
		return fmt.Errorf("%w: MP4 output route %d is unavailable", ErrMalformed, ordinal)
	}
	emitter, ok := output.Route(ordinal)
	if !ok || emitter == nil {
		return fmt.Errorf("%w: MP4 output route %d is unavailable", ErrMalformed, ordinal)
	}
	if uint64(value.size) > uint64(^uint(0)>>1) {
		return fmt.Errorf("%w: MP4 sample size exceeds runtime memory", ErrUnsupported)
	}
	lease, err := d.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: int(value.size)}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		return readMovieAt(ctx, d.reader, storage.Bytes(), value.offset, "sample payload")
	}); err != nil {
		return err
	}
	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	if value.sequence == 0 || value.dts > math.MaxInt64 {
		payload.Release()
		return fmt.Errorf("%w: MP4 sample timing is invalid", ErrMalformed)
	}
	packetValue := packet.NewPacket(
		value.sequence-1,
		timing.SomePTS(timing.NewPTS(value.pts)),
		timing.SomeDTS(timing.NewDTS(int64(value.dts))),
		timing.SomeDuration(timing.NewDuration(int64(value.duration))),
		payload,
	)
	item := &d.items[ordinal]
	emitter.Own(item, packetValue)
	defer item.Drop()
	if err := emitter.Emit(ctx, item); err != nil {
		return err
	}
	return nil
}
