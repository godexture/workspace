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

// demuxer reads one selected track per route, emitting samples in the order the
// source stores them. Keeping one cursor per track costs a fixed number of
// table pages per track and nothing per sample, and it means the muxer receives
// -- and so rewrites -- the movie in its own interleave rather than one track at
// a time. The comparison is between byte offsets inside one file, which is a
// total order the source already fixed; no timestamp is compared across tracks.
type demuxer struct {
	shape   flow.Shape
	tracks  []demuxTrack
	reader  access.Random
	buffers *buffer.Allocator
	items   []flow.Item[packet.Packet]
	cursors []trackCursor
	opened  bool
	done    bool
	failure error
}

// trackCursor is one track's reader plus the sample it will emit next, which is
// what the merge compares.
type trackCursor struct {
	cursor  sampleCursor
	pending sample
	more    bool
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
		cursors: make([]trackCursor, len(plan.tracks)),
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
	d.cursors = nil
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
	if err := d.open(ctx); err != nil {
		d.failure = err
		return err
	}
	ordinal := d.next()
	if ordinal < 0 {
		d.done = true
		return io.EOF
	}
	value := d.cursors[ordinal].pending
	if err := d.advance(ctx, ordinal); err != nil {
		d.failure = err
		return err
	}
	if err := d.emit(ctx, ordinal, value, output); err != nil {
		d.failure = err
		return err
	}
	return nil
}

func (d *demuxer) open(ctx context.Context) error {
	if d.opened {
		return nil
	}
	for ordinal := range d.cursors {
		cursor, err := newSampleCursor(ctx, d.reader, d.tracks[ordinal].value)
		if err != nil {
			return err
		}
		d.cursors[ordinal].cursor = cursor
		if err := d.advance(ctx, ordinal); err != nil {
			return err
		}
	}
	d.opened = true
	return nil
}

func (d *demuxer) advance(ctx context.Context, ordinal int) error {
	value, more, err := d.cursors[ordinal].cursor.next(ctx)
	if err != nil {
		return err
	}
	d.cursors[ordinal].pending, d.cursors[ordinal].more = value, more
	return nil
}

// next names the track holding the earliest stored sample. A tie can only
// happen between zero-length samples, and the lower ordinal wins so that the
// order stays a function of the file.
func (d *demuxer) next() int {
	result := -1
	for ordinal, value := range d.cursors {
		if !value.more {
			continue
		}
		if result < 0 || value.pending.offset < d.cursors[result].pending.offset {
			result = ordinal
		}
	}
	return result
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
