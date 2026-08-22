package mp4

import (
	"context"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
)

func (m *muxer) start(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.started {
		return nil
	}
	if err := m.sizeJournal(ctx); err != nil {
		return err
	}
	if err := m.emitPieces(ctx, m.layout.prefix(), output); err != nil {
		return err
	}
	m.outputOffset = m.layout.payloadOffset()
	m.started = true
	return nil
}

func (m *muxer) emitPieces(ctx context.Context, pieces []muxPiece, output flow.Emitter[access.Write]) error {
	for _, piece := range pieces {
		end, ok := checkedBoxAdd(piece.source, piece.size)
		if !ok {
			return fmt.Errorf("%w: MP4 output piece range overflows", ErrMalformed)
		}
		switch piece.kind {
		case muxCopy:
			if err := m.emitSourceSpan(ctx, piece.source, end, output); err != nil {
				return err
			}
		case muxHeader:
			if piece.size > uint64(len(piece.header)) {
				return fmt.Errorf("%w: MP4 output header piece is oversized", ErrMalformed)
			}
			if err := m.emitBytes(ctx, piece.header[:piece.size], output); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: MP4 output payload is written from packets", ErrMalformed)
		}
	}
	return nil
}

func (m *muxer) emitBytes(ctx context.Context, value []byte, output flow.Emitter[access.Write]) error {
	if len(value) == 0 {
		return nil
	}
	return m.emitFill(ctx, len(value), func(storage buffer.Mutable) error {
		copy(storage.Bytes(), value)
		return nil
	}, func(payload buffer.Handle) (access.Write, error) {
		return access.Append(payload)
	}, output)
}

func (m *muxer) emitSourceSpan(ctx context.Context, start, end uint64, output flow.Emitter[access.Write]) error {
	if start > end || end > m.movie.sourceEnd {
		return fmt.Errorf("%w: MP4 source preservation range is invalid", ErrMalformed)
	}
	for start < end {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		count := min(end-start, uint64(muxPageBytes))
		if start > math.MaxInt64 || count > uint64(math.MaxInt) {
			return fmt.Errorf("%w: MP4 source preservation range exceeds runtime offsets", ErrUnsupported)
		}
		if err := m.emitFill(ctx, int(count), func(storage buffer.Mutable) error {
			return readMovieAt(ctx, m.reader, storage.Bytes(), start, "preserved source range")
		}, func(payload buffer.Handle) (access.Write, error) {
			return access.Append(payload)
		}, output); err != nil {
			return err
		}
		start += count
	}
	return nil
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
