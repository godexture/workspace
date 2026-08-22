package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
)

func (m *muxer) appendChunkOffset(ctx context.Context) error {
	if m.scratch == nil || m.scratchWritten < 0 || m.scratchWritten > m.need || m.scratchPageUsed < 0 || m.scratchPageUsed > len(m.scratchPage)-8 {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	remaining := m.need - m.scratchWritten - int64(m.scratchPageUsed)
	if remaining < 8 {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	position := m.scratchPageUsed
	var previous [8]byte
	copy(previous[:], m.scratchPage[position:position+8])
	binary.BigEndian.PutUint64(m.scratchPage[position:position+8], m.outputOffset)
	used := m.scratchPageUsed + 8
	if used < len(m.scratchPage) {
		m.scratchPageUsed = used
		return nil
	}
	if err := m.appendScratchPage(ctx, used); err != nil {
		copy(m.scratchPage[position:position+8], previous[:])
		return err
	}
	return nil
}

func (m *muxer) flushScratchPage(ctx context.Context) error {
	if m.scratchPageUsed == 0 {
		return nil
	}
	if m.scratch == nil || m.scratchWritten < 0 || m.scratchWritten > m.need || m.scratchPageUsed < 0 || m.scratchPageUsed > len(m.scratchPage) {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	return m.appendScratchPage(ctx, m.scratchPageUsed)
}

func (m *muxer) appendScratchPage(ctx context.Context, used int) error {
	if used <= 0 || used > len(m.scratchPage) || int64(used) > m.need-m.scratchWritten {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	offset, err := m.scratch.Append(ctx, m.scratchPage[:used])
	if err != nil {
		return err
	}
	if offset != m.scratchWritten {
		return fmt.Errorf("%w: MP4 chunk-offset journal is not append-only", ErrMalformed)
	}
	m.scratchWritten += int64(used)
	m.scratchPageUsed = 0
	return nil
}

func (m *muxer) preflightOffsets(ctx context.Context) error {
	if m.scratchWritten != m.need {
		return fmt.Errorf("%w: MP4 chunk-offset journal cardinality differs", ErrMalformed)
	}
	if m.need == 0 {
		return nil
	}
	if m.scratch == nil {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	var position int64
	for _, selected := range m.layout.tracks {
		track := selected.value
		for remaining := uint64(track.chunkCount); remaining != 0; {
			count := min(remaining, uint64(muxJournalPageBytes/8))
			bytes := int(count * 8)
			if err := m.withScratchPage(bytes, func(values []byte) error {
				if err := m.scratch.ReadAt(ctx, values, position); err != nil {
					return err
				}
				if !track.tables.largeOffsets {
					for index := 0; index < len(values); index += 8 {
						if binary.BigEndian.Uint64(values[index:index+8]) > math.MaxUint32 {
							return fmt.Errorf("%w: remuxed stco offset exceeds uint32", ErrUnsupported)
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
			position += int64(bytes)
			remaining -= count
		}
	}
	if position != m.need {
		return fmt.Errorf("%w: MP4 chunk-offset journal cardinality differs", ErrMalformed)
	}
	return nil
}

func (m *muxer) patchOffsets(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.need == 0 {
		return nil
	}
	var position int64
	for _, selected := range m.layout.tracks {
		entrySize := 4
		if selected.value.tables.largeOffsets {
			entrySize = 8
		}
		patchOffset := selected.offsets
		for remaining := uint64(selected.value.chunkCount); remaining != 0; {
			count := min(remaining, uint64(muxJournalPageBytes/8))
			nextPatch, ok := checkedBoxAdd(patchOffset, count*uint64(entrySize))
			if !ok || nextPatch > math.MaxInt64 {
				return fmt.Errorf("%w: MP4 chunk-offset patch range overflows", ErrMalformed)
			}
			if err := m.emitOffsetPatch(ctx, position, int64(patchOffset), count, entrySize, output); err != nil {
				return err
			}
			patchOffset = nextPatch
			position += int64(count * 8)
			remaining -= count
		}
	}
	if position != m.need {
		return fmt.Errorf("%w: MP4 chunk-offset journal cardinality differs", ErrMalformed)
	}
	return nil
}

// patchDuration rewrites the mvhd duration when the selection drops a track.
func (m *muxer) patchDuration(ctx context.Context, output flow.Emitter[access.Write]) error {
	patch := m.layout.duration
	if patch.width == 0 {
		return nil
	}
	if patch.offset > math.MaxInt64 || (patch.width != 4 && patch.width != 8) || (patch.width == 4 && patch.value > math.MaxUint32) {
		return fmt.Errorf("%w: MP4 movie duration patch is invalid", ErrMalformed)
	}
	return m.emitFill(ctx, int(patch.width), func(storage buffer.Mutable) error {
		if patch.width == 4 {
			binary.BigEndian.PutUint32(storage.Bytes(), uint32(patch.value))
		} else {
			binary.BigEndian.PutUint64(storage.Bytes(), patch.value)
		}
		return nil
	}, func(payload buffer.Handle) (access.Write, error) {
		return access.Patch(int64(patch.offset), payload)
	}, output)
}

func (m *muxer) withScratchPage(size int, use func([]byte) error) error {
	lease, err := m.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: size}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	return lease.Fill(func(storage buffer.Mutable) error {
		return use(storage.Bytes())
	})
}

func (m *muxer) emitOffsetPatch(ctx context.Context, journalOffset, patchOffset int64, count uint64, entrySize int, output flow.Emitter[access.Write]) error {
	journalBytes := int(count * 8)
	patchBytes := int(count) * entrySize
	lease, err := m.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: journalBytes}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		values := storage.Bytes()
		if err := m.scratch.ReadAt(ctx, values, journalOffset); err != nil {
			return err
		}
		for index := 0; index < int(count); index++ {
			value := binary.BigEndian.Uint64(values[index*8 : index*8+8])
			if entrySize == 4 {
				if value > math.MaxUint32 {
					return fmt.Errorf("%w: remuxed stco offset exceeds uint32", ErrUnsupported)
				}
				binary.BigEndian.PutUint32(values[index*4:index*4+4], uint32(value))
			} else {
				binary.BigEndian.PutUint64(values[index*8:index*8+8], value)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	handle, err := lease.Commit()
	if err != nil {
		return err
	}
	payload, err := handle.Range(0, patchBytes)
	handle.Release()
	if err != nil {
		return err
	}
	write, err := access.Patch(patchOffset, payload)
	if err != nil {
		return err
	}
	output.Own(&m.out, write)
	defer m.out.Drop()
	return output.Emit(ctx, &m.out)
}
