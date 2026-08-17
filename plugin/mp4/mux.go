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

type muxer struct {
	out     flow.Item[access.Write]
	shape   flow.Shape
	movie   movie
	reader  access.Random
	buffers *buffer.Allocator
	scratch plugin.Scratch
	need    int64

	track     int
	cursor    sampleCursor
	hasCursor bool

	started        bool
	finalized      bool
	flushed        bool
	payloadBytes   uint64
	outputOffset   uint64
	scratchWritten int64
	failure        error
}

func openMuxer(ctx plugin.OpenContext, plan muxPlan) (*muxer, error) {
	if !plan.shape.Equal(muxerShape()) || validateMuxMovie(plan.movie) != nil {
		return nil, fmt.Errorf("%w: MP4 mux plan is invalid", ErrMalformed)
	}
	need, err := muxScratchBytes(plan.movie)
	if err != nil || need != plan.scratch || uint64(need) > math.MaxInt64 {
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
		reader:  reader,
		buffers: ctx.Buffers(),
		scratch: ctx.Scratch(),
		need:    int64(need),
	}, nil
}

func (m *muxer) Ports() flow.Shape { return m.shape.Clone() }

func (m *muxer) Close() error {
	m.reader = nil
	m.buffers = nil
	m.scratch = nil
	m.cursor.reader = nil
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
	expected, more, err := m.cursor.next(ctx)
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
		outputOffset = m.movie.media.payloadOffset
	}
	if uint64(expected.size) > math.MaxUint64-m.payloadBytes || uint64(expected.size) > math.MaxUint64-outputOffset {
		return m.fail(fmt.Errorf("%w: MP4 output offset overflows", ErrUnsupported))
	}
	if err := m.start(ctx, output); err != nil {
		return m.fail(err)
	}
	if expected.chunkStart {
		if err := m.appendChunkOffset(ctx); err != nil {
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
	if m.cursor.sequence == m.cursor.track.sampleCount {
		m.hasCursor = false
		m.track++
	}
	return nil
}

func (m *muxer) Finalize(ctx context.Context) error {
	if m.failure != nil {
		return m.failure
	}
	if m.finalized {
		return errors.New("MP4 muxer was finalized more than once")
	}
	if err := context.Cause(ctx); err != nil {
		return m.fail(err)
	}
	m.skipEmptyTracks()
	if m.hasCursor || m.track != len(m.movie.tracks) {
		return m.fail(fmt.Errorf("%w: MP4 muxer did not receive every inspected sample", ErrUnsupported))
	}
	if m.payloadBytes != m.movie.media.payloadSize {
		return m.fail(fmt.Errorf("%w: MP4 muxer payload does not cover mdat", ErrUnsupported))
	}
	if m.scratchWritten != m.need {
		return m.fail(fmt.Errorf("%w: MP4 muxer chunk-offset journal is incomplete", ErrMalformed))
	}
	m.finalized = true
	return nil
}

func (m *muxer) Flush(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.failure != nil {
		return m.failure
	}
	if !m.finalized {
		return errors.New("MP4 muxer must be finalized before flush")
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
	if err := m.preflightOffsets(ctx); err != nil {
		return m.fail(err)
	}
	mediaEnd, ok := payloadEnd(m.movie.media)
	if !ok {
		return m.fail(fmt.Errorf("%w: MP4 mdat payload range overflows", ErrMalformed))
	}
	if err := m.emitSourceSpan(ctx, mediaEnd, m.movie.sourceEnd, output); err != nil {
		return m.fail(err)
	}
	if err := m.patchOffsets(ctx, output); err != nil {
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
	m.skipEmptyTracks()
	if ordinal != m.track || ordinal < 0 || ordinal >= len(m.movie.tracks) {
		return fmt.Errorf("%w: MP4 packet input %d is not the next inspected track", ErrUnsupported, ordinal)
	}
	if m.hasCursor {
		return nil
	}
	cursor, err := newSampleCursor(ctx, m.reader, m.movie.tracks[ordinal])
	if err != nil {
		return err
	}
	m.cursor = cursor
	m.hasCursor = true
	return nil
}

func (m *muxer) skipEmptyTracks() {
	for !m.hasCursor && m.track < len(m.movie.tracks) && m.movie.tracks[m.track].sampleCount == 0 {
		m.track++
	}
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
