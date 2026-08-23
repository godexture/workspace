package mp4

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/plugin"
)

// muxer rebuilds the mdat payload from the packets as they arrive and patches
// the tables that record where the bytes landed. The arriving order is the
// reader's own emit order, which is the order the source stored the samples in,
// so an interleaved movie stays interleaved.
type muxer struct {
	out     flow.Item[access.Write]
	shape   flow.Shape
	movie   movie
	layout  muxLayout
	reader  access.Random
	buffers *buffer.Allocator
	scratch plugin.Scratch
	need    int64

	tracks []muxCursor

	started      bool
	sized        bool
	finalized    bool
	flushed      bool
	payloadBytes uint64
	outputOffset uint64
	failure      error
}

// muxCursor holds one track's expected samples and the chunk offsets it has
// recorded so far. Entries are buffered per track and written into that track's
// journal region, because chunks from different tracks now arrive interleaved.
type muxCursor struct {
	cursor   sampleCursor
	opened   bool
	page     [muxJournalTrackPageBytes]byte
	used     int
	recorded uint32
}

func openMuxer(ctx plugin.OpenContext, plan muxPlan) (*muxer, error) {
	if !plan.shape.Equal(muxerShape()) || validateMuxLayout(plan.movie, plan.layout) != nil {
		return nil, fmt.Errorf("%w: MP4 mux plan is invalid", ErrMalformed)
	}
	need := plan.layout.journalBytes()
	if need != plan.scratch || uint64(need) > math.MaxInt64 {
		return nil, fmt.Errorf("%w: MP4 mux scratch plan is invalid", ErrMalformed)
	}
	if ctx.Buffers() == nil {
		return nil, fmt.Errorf("%w: MP4 muxer requires a source-page buffer grant", ErrUnsupported)
	}
	opening, ok := mediaformat.SourceOpening(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: MP4 muxer requires its inspected source opening", ErrUnsupported)
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
	if size < 0 || uint64(size) != plan.movie.sourceEnd {
		return nil, fmt.Errorf("%w: MP4 source size changed after inspection", ErrMalformed)
	}
	if need != 0 && ctx.Scratch() == nil {
		return nil, fmt.Errorf("%w: MP4 muxer requires its chunk-offset journal", ErrUnsupported)
	}
	return &muxer{
		shape:   plan.shape.Clone(),
		movie:   plan.movie,
		layout:  plan.layout,
		reader:  reader,
		buffers: ctx.Buffers(),
		scratch: ctx.Scratch(),
		need:    int64(need),
		tracks:  make([]muxCursor, len(plan.layout.tracks)),
	}, nil
}

func (m *muxer) Ports() flow.Shape { return m.shape.Clone() }

func (m *muxer) Close() error {
	m.reader = nil
	m.buffers = nil
	m.scratch = nil
	m.tracks = nil
	return nil
}

func (m *muxer) Process(ctx context.Context, batch flow.Batch[packet.Packet], output flow.Emitter[access.Write]) error {
	item := batch.At(0)
	if item != nil {
		defer item.Drop()
	}
	if m.failure != nil {
		return m.failure
	}
	if m.finalized || m.flushed {
		return m.fail(errors.New("MP4 muxer received a packet after finalization"))
	}
	ordinal, selected := batch.Input()
	if !selected || batch.Len() != 1 || item == nil || !item.Valid() || !item.Value().Valid() {
		return m.fail(fmt.Errorf("%w: MP4 muxer requires one selected owned packet", ErrMalformed))
	}
	if err := context.Cause(ctx); err != nil {
		return m.fail(err)
	}
	if err := m.selectTrack(ctx, ordinal); err != nil {
		return m.fail(err)
	}
	expected, more, err := m.tracks[ordinal].cursor.next(ctx)
	if err != nil {
		return m.fail(err)
	}
	if !more {
		return m.fail(fmt.Errorf("%w: packet input %d exceeds inspected sample count", ErrUnsupported, ordinal))
	}
	if err := validateMuxPacket(item.Value(), expected); err != nil {
		return m.fail(err)
	}
	outputOffset := m.outputOffset
	if !m.started {
		outputOffset = m.layout.payloadOffset()
	}
	if uint64(expected.size) > math.MaxUint64-m.payloadBytes || uint64(expected.size) > math.MaxUint64-outputOffset {
		return m.fail(fmt.Errorf("%w: MP4 output offset overflows", ErrUnsupported))
	}
	// A movie carrying byte offsets outside the sample tables was accepted on
	// the promise that nothing moves. The layout placed the payload; the
	// arrival order decides the rest, so it is checked here rather than assumed
	// from the topology that produced it.
	if m.layout.verbatim && expected.offset != outputOffset {
		return m.fail(fmt.Errorf("%w: MP4 sample %d moved from %d to %d, and this movie records byte offsets that would go stale", ErrUnsupported, expected.sequence, expected.offset, outputOffset))
	}
	if err := m.start(ctx, output); err != nil {
		return m.fail(err)
	}
	if expected.chunkStart {
		if err := m.recordChunkOffset(ctx, ordinal); err != nil {
			return m.fail(err)
		}
	}
	if err := flow.Transfer(item, &m.out, output, func(value packet.Packet) (access.Write, error) {
		return access.Append(value.Detach())
	}); err != nil {
		return m.fail(err)
	}
	defer m.out.Drop()
	if err := output.Emit(ctx, &m.out); err != nil {
		return m.fail(err)
	}
	m.payloadBytes += uint64(expected.size)
	m.outputOffset += uint64(expected.size)
	return nil
}

func (m *muxer) finalize(ctx context.Context) error {
	if m.failure != nil {
		return m.failure
	}
	if m.finalized {
		return nil
	}
	if err := context.Cause(ctx); err != nil {
		return m.fail(err)
	}
	for ordinal, selected := range m.layout.tracks {
		if m.tracks[ordinal].cursor.sequence != selected.value.sampleCount {
			return m.fail(fmt.Errorf("%w: MP4 muxer did not receive every inspected sample", ErrUnsupported))
		}
		if err := m.flushChunkOffsets(ctx, ordinal); err != nil {
			return m.fail(err)
		}
		if m.tracks[ordinal].recorded != selected.value.chunkCount {
			return m.fail(fmt.Errorf("%w: MP4 muxer chunk-offset journal is incomplete", ErrMalformed))
		}
	}
	if m.payloadBytes != m.layout.payloadSize() {
		return m.fail(fmt.Errorf("%w: MP4 muxer payload does not cover mdat", ErrUnsupported))
	}
	m.finalized = true
	return nil
}

// Flush states what only the whole of the input decides. It runs after every
// node above it has flushed, so the sample tables it rebuilds are complete
// even when a coder upstream emitted its last packet during its own.
func (m *muxer) Flush(ctx context.Context, output flow.Emitter[access.Write]) error {
	if err := m.finalize(ctx); err != nil {
		return err
	}
	if m.flushed {
		return nil
	}
	if err := context.Cause(ctx); err != nil {
		return m.fail(err)
	}
	if err := m.start(ctx, output); err != nil {
		return m.fail(err)
	}
	if err := m.emitPieces(ctx, m.layout.suffix(), output); err != nil {
		return m.fail(err)
	}
	if err := m.patchOffsets(ctx, output); err != nil {
		return m.fail(err)
	}
	if err := m.patchDuration(ctx, output); err != nil {
		return m.fail(err)
	}
	m.flushed = true
	return nil
}

func (m *muxer) fail(err error) error {
	if err != nil && m.failure == nil {
		m.failure = err
	}
	return err
}

func (m *muxer) selectTrack(ctx context.Context, ordinal int) error {
	if ordinal < 0 || ordinal >= len(m.layout.tracks) {
		return fmt.Errorf("%w: MP4 packet input %d is not a selected track", ErrUnsupported, ordinal)
	}
	if m.tracks[ordinal].opened {
		return nil
	}
	cursor, err := newSampleCursor(ctx, m.reader, m.layout.tracks[ordinal].value)
	if err != nil {
		return err
	}
	m.tracks[ordinal].cursor = cursor
	m.tracks[ordinal].opened = true
	return nil
}

func validateMuxPacket(value packet.Packet, expected sample) error {
	if value.Sequence() != expected.sequence-1 {
		return fmt.Errorf("%w: packet sequence %d does not match inspected sample %d", ErrUnsupported, value.Sequence(), expected.sequence-1)
	}
	pts, havePTS := value.PTS().Get()
	dts, haveDTS := value.DTS().Get()
	duration, haveDuration := value.Duration().Get()
	if !havePTS || !haveDTS || !haveDuration || pts.Int64() != expected.pts || dts.Int64() != int64(expected.dts) || duration.Int64() != int64(expected.duration) {
		return fmt.Errorf("%w: packet timing does not match inspected sample %d", ErrUnsupported, expected.sequence)
	}
	if value.SideData().Valid() || uint64(value.Bytes().Len()) != uint64(expected.size) {
		return fmt.Errorf("%w: packet payload does not match inspected sample %d", ErrUnsupported, expected.sequence)
	}
	return nil
}
