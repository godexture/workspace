package wave

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
)

type muxer struct {
	out           flow.Item[access.Write]
	shape         flow.Shape
	buffers       *buffer.Allocator
	header        muxHeader
	dataSize      uint64
	headerEmitted bool
	finalized     bool
	flushed       bool
	source        access.Random
}

func newMuxer(plan muxPlan, buffers *buffer.Allocator) *muxer {
	return &muxer{shape: plan.shape.Clone(), buffers: buffers, header: plan.header}
}

func (m *muxer) setSource(ctx context.Context, opening access.Opening) error {
	if !m.header.rangeMode {
		return nil
	}
	random, ok := access.RandomOf(opening)
	if !ok {
		return fmt.Errorf("%w: WAVE range mux requires a RandomRead source opening", ErrUnsupported)
	}
	sizer, ok := access.StableSizeOf(opening)
	if !ok {
		return fmt.Errorf("%w: WAVE range mux requires a StableSize source opening", ErrUnsupported)
	}
	size, err := sizer.Size(ctx)
	if err != nil {
		return err
	}
	if size < 0 || uint64(size) < m.header.sourceEnd() {
		return fmt.Errorf("%w: WAVE source opening is shorter than an inspected preservation range", ErrTruncatedData)
	}
	m.source = random
	return nil
}

func (m *muxer) Ports() flow.Shape { return m.shape.Clone() }
func (m *muxer) Close() error {
	m.source = nil
	return nil
}

func (m *muxer) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[access.Write]) error {
	defer input.Drop()
	if m.finalized {
		return errors.New("WAVE muxer received a packet after finalization")
	}
	if !input.Valid() || !input.Value().Valid() {
		return errors.New("WAVE muxer received an invalid packet")
	}
	payload := input.Value().Payload()
	size := payload.Bytes().Len()
	if uint64(size)%m.header.blockAlign != 0 {
		return ErrPartialBlock
	}
	if uint64(size) > math.MaxUint64-m.dataSize {
		return fmt.Errorf("%w: WAVE data size overflows", ErrUnsupported)
	}
	nextSize := m.dataSize + uint64(size)
	if _, _, err := m.header.outputSize(nextSize); err != nil {
		return err
	}
	if err := m.emitHeader(ctx, output); err != nil {
		return err
	}
	if err := flow.Transfer(input, &m.out, output, func(value packet.Packet) (access.Write, error) {
		return access.Append(value.Detach())
	}); err != nil {
		return err
	}
	defer m.out.Drop()
	if err := output.Emit(ctx, &m.out); err != nil {
		return err
	}
	m.dataSize = nextSize
	return nil
}

func (m *muxer) finalize() error {
	if m.finalized {
		return nil
	}
	if m.dataSize%m.header.blockAlign != 0 {
		return ErrPartialBlock
	}
	m.finalized = true
	return nil
}

// Flush states what only the whole of the input decides. It runs after every
// node above it has flushed, so the payload size it patches into the header is
// final even when a coder upstream emitted its last block during its own.
func (m *muxer) Flush(ctx context.Context, output flow.Emitter[access.Write]) error {
	if err := m.finalize(); err != nil {
		return err
	}
	if m.flushed {
		return nil
	}
	if err := m.emitHeader(ctx, output); err != nil {
		return err
	}
	finalized, err := m.header.finalize(m.dataSize)
	if err != nil {
		return err
	}
	if finalized.padding != 0 {
		if err := m.emitAppend(ctx, []byte{0}, output); err != nil {
			return err
		}
	}
	if m.header.rangeMode {
		if err := m.emitRange(ctx, m.header.ranges.afterData, output); err != nil {
			return err
		}
		if err := m.emitRange(ctx, m.header.ranges.trailer, output); err != nil {
			return err
		}
	} else {
		if len(m.header.afterData) != 0 {
			if err := m.emitAppend(ctx, m.header.afterData, output); err != nil {
				return err
			}
		}
		if len(m.header.trailer) != 0 {
			if err := m.emitAppend(ctx, m.header.trailer, output); err != nil {
				return err
			}
		}
	}
	for _, patch := range finalized.patches {
		if err := m.emitPatch(ctx, patch, output); err != nil {
			return err
		}
	}
	m.flushed = true
	return nil
}

func (m *muxer) emitHeader(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.headerEmitted {
		return nil
	}
	if m.header.rangeMode {
		if err := m.emitAppend(ctx, m.header.prefix, output); err != nil {
			return err
		}
		if m.header.ranges.reservation.valid() {
			if err := m.emitRange(ctx, m.header.ranges.reservation, output); err != nil {
				return err
			}
		} else if err := m.emitAppend(ctx, reserveChunkOf(muxChunks{}), output); err != nil {
			return err
		}
		if err := m.emitRange(ctx, m.header.ranges.beforeFormat, output); err != nil {
			return err
		}
		if err := m.emitAppend(ctx, m.header.format, output); err != nil {
			return err
		}
		if err := m.emitRange(ctx, m.header.ranges.beforeData, output); err != nil {
			return err
		}
		if err := m.emitAppend(ctx, m.header.dataTag, output); err != nil {
			return err
		}
	} else if err := m.emitAppend(ctx, m.header.initial, output); err != nil {
		return err
	}
	m.headerEmitted = true
	return nil
}

func (m *muxer) emitRange(ctx context.Context, value sourceRange, output flow.Emitter[access.Write]) error {
	if !value.valid() {
		return nil
	}
	if m.source == nil {
		return fmt.Errorf("%w: WAVE source range is unavailable at Open", ErrUnsupported)
	}
	start := value.offset
	end, ok := value.end()
	if !ok || start > math.MaxInt64 || end > math.MaxInt64+1 {
		return fmt.Errorf("%w: WAVE source range exceeds runtime offsets", ErrUnsupported)
	}
	if m.header.replacement.source.valid() && rangesOverlap(value, m.header.replacement.source) {
		replacement := m.header.replacement.source
		replacementEnd, _ := replacement.end()
		if replacement.offset < start || replacementEnd > end {
			return fmt.Errorf("%w: WAVE source replacement is outside its anchor range", ErrMalformed)
		}
		if err := m.emitSourceSpan(ctx, start, replacement.offset, output); err != nil {
			return err
		}
		if err := m.emitAppend(ctx, m.header.replacement.payload, output); err != nil {
			return err
		}
		start = replacementEnd
	}
	return m.emitSourceSpan(ctx, start, end, output)
}

func rangesOverlap(left, right sourceRange) bool {
	leftEnd, leftOK := left.end()
	rightEnd, rightOK := right.end()
	return leftOK && rightOK && left.offset < rightEnd && right.offset < leftEnd
}

func (m *muxer) emitSourceSpan(ctx context.Context, start, end uint64, output flow.Emitter[access.Write]) error {
	if start >= end {
		return nil
	}
	for start < end {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		remaining := end - start
		count := uint64(wavePageSize)
		if count > remaining {
			count = remaining
		}
		if count > uint64(math.MaxInt) || start > math.MaxInt64 {
			return fmt.Errorf("%w: WAVE source read exceeds runtime offsets", ErrUnsupported)
		}
		if err := m.emitSourcePage(ctx, int64(start), int(count), output); err != nil {
			return err
		}
		start += count
	}
	return nil
}

func (m *muxer) emitSourcePage(ctx context.Context, offset int64, size int, output flow.Emitter[access.Write]) error {
	return m.emitFill(ctx, size, func(storage buffer.Mutable) error {
		if err := access.ReadFullAt(ctx, m.source, storage.Bytes(), offset); err != nil {
			return fmt.Errorf("%w: WAVE source range at %d: %w", ErrTruncatedData, offset, err)
		}
		return nil
	}, func(handle buffer.Handle) (access.Write, error) {
		return access.Append(handle)
	}, output)
}

func (m *muxer) emitAppend(ctx context.Context, payload []byte, output flow.Emitter[access.Write]) error {
	return m.emit(ctx, payload, func(handle buffer.Handle) (access.Write, error) {
		return access.Append(handle)
	}, output)
}

func (m *muxer) emitPatch(ctx context.Context, patch headerPatch, output flow.Emitter[access.Write]) error {
	return m.emit(ctx, patch.payload, func(handle buffer.Handle) (access.Write, error) {
		return access.Patch(patch.offset, handle)
	}, output)
}

func (m *muxer) emit(ctx context.Context, payload []byte, build func(buffer.Handle) (access.Write, error), output flow.Emitter[access.Write]) error {
	return m.emitFill(ctx, len(payload), func(storage buffer.Mutable) error {
		copy(storage.Bytes(), payload)
		return nil
	}, build, output)
}

func (m *muxer) emitFill(ctx context.Context, size int, fill func(buffer.Mutable) error, build func(buffer.Handle) (access.Write, error), output flow.Emitter[access.Write]) error {
	lease, err := m.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: size}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(fill); err != nil {
		return err
	}
	handle, err := lease.Commit()
	if err != nil {
		return err
	}
	write, err := build(handle)
	if err != nil {
		return err
	}
	output.Own(&m.out, write)
	defer m.out.Drop()
	return output.Emit(ctx, &m.out)
}
