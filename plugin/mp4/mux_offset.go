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

// sizeJournal reserves the whole journal before any track records into it.
// Tracks fill their own regions out of order, so the bytes have to exist first;
// Append is the only call that grows the journal.
func (m *muxer) sizeJournal(ctx context.Context) error {
	if m.sized || m.need == 0 {
		m.sized = true
		return nil
	}
	if m.scratch == nil {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	var page [muxJournalPageBytes]byte
	for written := int64(0); written < m.need; {
		count := min(m.need-written, int64(len(page)))
		offset, err := m.scratch.Append(ctx, page[:count])
		if err != nil {
			return err
		}
		if offset != written {
			return fmt.Errorf("%w: MP4 chunk-offset journal is not append-only", ErrMalformed)
		}
		written += count
	}
	m.sized = true
	return nil
}

// recordChunkOffset notes where this track's next chunk starts in the output.
// Entries are held per track and written a page at a time, because a chunk of
// another track usually arrives in between.
func (m *muxer) recordChunkOffset(ctx context.Context, ordinal int) error {
	if m.scratch == nil || !m.sized || ordinal < 0 || ordinal >= len(m.tracks) {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	track := &m.tracks[ordinal]
	if track.recorded >= m.layout.tracks[ordinal].value.chunkCount || track.used < 0 || track.used > len(track.page)-8 {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	binary.BigEndian.PutUint64(track.page[track.used:track.used+8], m.outputOffset)
	track.used += 8
	track.recorded++
	if track.used < len(track.page) {
		return nil
	}
	return m.flushChunkOffsets(ctx, ordinal)
}

func (m *muxer) flushChunkOffsets(ctx context.Context, ordinal int) error {
	track := &m.tracks[ordinal]
	if track.used == 0 {
		return nil
	}
	if m.scratch == nil || track.used%8 != 0 || uint64(track.used/8) > uint64(track.recorded) {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	position, ok := checkedBoxAdd(m.layout.tracks[ordinal].journal, (uint64(track.recorded)-uint64(track.used/8))*8)
	end, endOK := checkedBoxAdd(position, uint64(track.used))
	if !ok || !endOK || end > uint64(m.need) || position > math.MaxInt64 {
		return fmt.Errorf("%w: MP4 chunk-offset journal is unavailable", ErrMalformed)
	}
	if err := m.scratch.WriteAt(ctx, track.page[:track.used], int64(position)); err != nil {
		return err
	}
	track.used = 0
	return nil
}

func (m *muxer) patchOffsets(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.need == 0 {
		return nil
	}
	var covered uint64
	for _, selected := range m.layout.tracks {
		entrySize := 4
		if selected.value.tables.largeOffsets {
			entrySize = 8
		}
		patchOffset := selected.offsets
		position := selected.journal
		for remaining := uint64(selected.value.chunkCount); remaining != 0; {
			count := min(remaining, uint64(muxJournalPageBytes/8))
			nextPatch, ok := checkedBoxAdd(patchOffset, count*uint64(entrySize))
			if !ok || nextPatch > math.MaxInt64 || position > math.MaxInt64 {
				return fmt.Errorf("%w: MP4 chunk-offset patch range overflows", ErrMalformed)
			}
			if err := m.emitOffsetPatch(ctx, int64(position), int64(patchOffset), count, entrySize, output); err != nil {
				return err
			}
			patchOffset = nextPatch
			position += count * 8
			covered += count * 8
			remaining -= count
		}
	}
	if covered != uint64(m.need) {
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
