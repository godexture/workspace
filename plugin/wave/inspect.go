package wave

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

func inspectWAVE(ctx mediaformat.InspectContext) (mediaformat.Inspection, error) {
	random, ok := access.RandomOf(ctx.Opening())
	if !ok {
		return mediaformat.Inspection{}, fmt.Errorf("%w: WAVE Inspect requires random read", ErrUnsupported)
	}
	sizer, ok := access.StableSizeOf(ctx.Opening())
	if !ok {
		return mediaformat.Inspection{}, fmt.Errorf("%w: WAVE Inspect requires stable size", ErrUnsupported)
	}
	size, err := sizer.Size(ctx.Context())
	if err != nil {
		return mediaformat.Inspection{}, err
	}
	if size < 0 {
		return mediaformat.Inspection{}, fmt.Errorf("%w: stable source size is negative", ErrMalformed)
	}
	resolver, _ := metadata.ResolverOf(ctx.Prepared())
	value, err := inspectHeaderWithSize(ctx.Context(), random, uint64(size), true, resolver, ctx.MemoryLimit())
	if err != nil {
		return mediaformat.Inspection{}, err
	}
	return mediaformat.NewClonedInspection(WAVE(), value, func(value header) header { return value }), nil
}

func inspectHeader(ctx context.Context, reader access.Random) (header, error) {
	return inspectHeaderWithMetadata(ctx, reader, metadata.Resolver{})
}

func inspectHeaderWithMetadata(ctx context.Context, reader access.Random, resolver metadata.Resolver) (header, error) {
	return inspectHeaderWithSize(ctx, reader, 0, false, resolver, job.DefaultBudget().InspectMemory)
}

func inspectHeaderWithSize(ctx context.Context, reader access.Random, sourceSize uint64, sizeKnown bool, resolver metadata.Resolver, memoryLimit resource.Bytes) (header, error) {
	budget := preserveBudget{remaining: uint64(memoryLimit)}
	if reader == nil {
		return header{}, fmt.Errorf("%w: random reader is nil", ErrMalformed)
	}
	if !resolver.Valid() {
		resolver, _ = metadata.NewResolver(nil)
	}
	var root [12]byte
	if err := access.ReadFullAt(ctx, reader, root[:], 0); err != nil {
		return header{}, fmt.Errorf("%w: RIFF header: %w", ErrMalformed, err)
	}
	rf64 := string(root[0:4]) == tagRF64
	if string(root[0:4]) != tagRIFF && !rf64 || string(root[8:12]) != tagWAVE {
		return header{}, fmt.Errorf("%w: RIFF/WAVE signature is absent", ErrMalformed)
	}
	rootSize := uint64(binary.LittleEndian.Uint32(root[4:8]))
	if rootSize < 4 || rf64 && rootSize != math.MaxUint32 {
		return header{}, fmt.Errorf("%w: invalid RIFF size", ErrMalformed)
	}
	rootEnd := uint64(8) + rootSize
	if rf64 {
		rootEnd = 0
	} else if err := validateSourceEnd(rootEnd, sourceSize, sizeKnown); err != nil {
		return header{}, err
	}

	var result header
	result.rf64 = rf64
	result.codecTag = PCMTag()
	document := metadata.NewBuilder(metadata.StreamScope)
	var formatFound, dataFound, ds64Found bool
	var ds64DataSize uint64
	offset := uint64(12)
	for chunks := 0; chunks < 1<<20; chunks++ {
		if rootEnd != 0 {
			if offset == rootEnd {
				break
			}
			if offset > rootEnd || rootEnd-offset < 8 {
				return header{}, fmt.Errorf("%w: chunk header exceeds RIFF size", ErrMalformed)
			}
		}
		if offset > math.MaxInt64 {
			return header{}, fmt.Errorf("%w: chunk offset exceeds runtime range", ErrUnsupported)
		}
		var chunk [8]byte
		if err := access.ReadFullAt(ctx, reader, chunk[:], int64(offset)); err != nil {
			return header{}, fmt.Errorf("%w: chunk header at %d: %w", ErrMalformed, offset, err)
		}
		id := string(chunk[0:4])
		declaredSize := uint64(binary.LittleEndian.Uint32(chunk[4:8]))
		payloadOffset, ok := checkedAdd(offset, 8)
		if !ok {
			return header{}, fmt.Errorf("%w: chunk payload offset overflows", ErrMalformed)
		}
		if payloadOffset > math.MaxInt64 {
			return header{}, fmt.Errorf("%w: chunk payload offset exceeds runtime range", ErrUnsupported)
		}
		actualSize := declaredSize
		anchor := chunkAnchorAt(formatFound, dataFound)
		preserve := true

		// A JUNK chunk of exactly ds64 size in the reservation slot is where an
		// RF64 writer keeps room for a later ds64, and this writer recreates
		// that slot. Its payload is still input-derived content and is legal
		// non-zero, so it is preserved and written back into the same slot
		// instead of being regenerated as zeros.
		if id == tagJUNK && offset == reserveOffset && declaredSize == ds64PayloadSize {
			anchor = chunkReservation
		}

		switch id {
		case tagDS64:
			preserve = false
			if ds64Found || declaredSize < 28 {
				return header{}, fmt.Errorf("%w: invalid ds64 chunk", ErrMalformed)
			}
			var payload [28]byte
			if err := access.ReadFullAt(ctx, reader, payload[:], int64(payloadOffset)); err != nil {
				return header{}, fmt.Errorf("%w: ds64 chunk: %w", ErrMalformed, err)
			}
			riffSize := binary.LittleEndian.Uint64(payload[0:8])
			ds64DataSize = binary.LittleEndian.Uint64(payload[8:16])
			if riffSize < 4 || riffSize > math.MaxInt64-8 {
				return header{}, fmt.Errorf("%w: ds64 RIFF size is invalid", ErrMalformed)
			}
			if rf64 {
				rootEnd = 8 + riffSize
				if err := validateSourceEnd(rootEnd, sourceSize, sizeKnown); err != nil {
					return header{}, err
				}
			}
			ds64Found = true
		case tagFMT:
			preserve = false
			if formatFound {
				return header{}, fmt.Errorf("%w: fmt chunk is repeated", ErrMalformed)
			}
			description, blockAlign, err := inspectFormat(ctx, reader, payloadOffset, declaredSize)
			if err != nil {
				return header{}, err
			}
			result.description = description
			result.blockAlign = blockAlign
			formatFound = true
		case tagDATA:
			preserve = false
			if dataFound {
				return header{}, fmt.Errorf("%w: data chunk is repeated", ErrMalformed)
			}
			if declaredSize == math.MaxUint32 {
				if !rf64 || !ds64Found {
					return header{}, fmt.Errorf("%w: extended data size has no ds64 chunk", ErrMalformed)
				}
				actualSize = ds64DataSize
			}
			if actualSize > uint64(math.MaxInt64)-payloadOffset {
				return header{}, fmt.Errorf("%w: data range exceeds runtime offsets", ErrUnsupported)
			}
			dataEnd := payloadOffset + actualSize
			if sizeKnown && dataEnd > sourceSize {
				return header{}, fmt.Errorf("%w: data ends at %d, source size is %d", ErrTruncatedData, dataEnd, sourceSize)
			}
			result.dataOffset = int64(payloadOffset)
			result.dataSize = actualSize
			dataFound = true
		}

		next, ok := checkedAdd(payloadOffset, actualSize)
		if !ok {
			return header{}, fmt.Errorf("%w: chunk size overflows", ErrMalformed)
		}
		if actualSize&1 != 0 {
			next, ok = checkedAdd(next, 1)
			if !ok {
				return header{}, fmt.Errorf("%w: chunk padding overflows", ErrMalformed)
			}
		}
		if next <= offset || rootEnd != 0 && next > rootEnd {
			return header{}, fmt.Errorf("%w: chunk exceeds RIFF bounds", ErrMalformed)
		}
		if preserve {
			if err := inspectPreservedChunk(ctx, reader, resolver, document, &budget, offset, next, declaredSize, id, anchor); err != nil {
				return header{}, err
			}
		}
		offset = next
	}
	if rf64 && !ds64Found {
		return header{}, fmt.Errorf("%w: RF64 stream has no ds64 chunk", ErrMalformed)
	}
	if !formatFound || !dataFound {
		return header{}, fmt.Errorf("%w: fmt or data chunk is absent", ErrMalformed)
	}
	if rootEnd == 0 || offset != rootEnd {
		return header{}, fmt.Errorf("%w: RIFF chunk scan did not reach the declared end", ErrMalformed)
	}
	if sizeKnown && sourceSize > rootEnd {
		if err := inspectTrailer(ctx, reader, document, &budget, rootEnd, sourceSize); err != nil {
			return header{}, err
		}
	}
	parsed, err := document.Build()
	if err != nil {
		return header{}, fmt.Errorf("%w: WAVE metadata document: %w", ErrMalformed, err)
	}
	result.metadata = parsed
	if !result.valid() {
		return header{}, fmt.Errorf("%w: PCM description and data block disagree", ErrMalformed)
	}
	return result, nil
}

// validateSourceEnd rejects a RIFF chunk that claims more than the source
// holds. Bytes past the chunk are not an error: appended tags and encoder
// padding are common, and the spec makes the size field the chunk boundary,
// not the file boundary. Inspect preserves that region instead.
func validateSourceEnd(declared, actual uint64, known bool) error {
	if !known || declared <= actual {
		return nil
	}
	return fmt.Errorf("%w: RIFF ends at %d, source size is %d", ErrTruncatedData, declared, actual)
}

func inspectPreservedChunk(ctx context.Context, reader access.Random, resolver metadata.Resolver, builder *metadata.Builder, budget *preserveBudget, offset, next, declaredSize uint64, id string, anchor chunkAnchor) error {
	length := next - offset
	if length > uint64(^uint(0)>>1) {
		return fmt.Errorf("%w: chunk %q exceeds control-plane address space", ErrUnsupported, id)
	}
	if err := budget.reserve(length, id); err != nil {
		return err
	}
	raw := make([]byte, int(length))
	if err := access.ReadFullAt(ctx, reader, raw, int64(offset)); err != nil {
		return fmt.Errorf("%w: preserved chunk %q at %d: %w", ErrMalformed, id, offset, err)
	}
	if id == tagLIST && declaredSize >= 4 && len(raw) >= 12 && string(raw[8:12]) == tagINFO {
		block := newChunkBlockID(offset, anchor, chunkInfo)
		document, err := resolver.Parse(ctx, RIFFInfo(), block, metadata.StreamScope, metadata.NewBlob("application/x-riff-info", raw))
		if err != nil {
			return err
		}
		builder.Append(document)
		return nil
	}
	block := metadata.NewRawBlock(
		newChunkBlockID(offset, anchor, chunkRaw),
		rawChunkCarrier(),
		plugin.Identity{},
		metadata.NewBlob("application/x-riff-chunk", raw),
	)
	builder.AddBlock(block)
	return nil
}

func inspectFormat(ctx context.Context, reader access.Random, offset, size uint64) (sample.Description, int, error) {
	if size < 16 {
		return sample.Description{}, 0, fmt.Errorf("%w: fmt chunk is shorter than 16 bytes", ErrMalformed)
	}
	readSize := size
	if readSize > 40 {
		readSize = 40
	}
	buffer := make([]byte, int(readSize))
	if err := access.ReadFullAt(ctx, reader, buffer, int64(offset)); err != nil {
		return sample.Description{}, 0, fmt.Errorf("%w: fmt chunk: %w", ErrMalformed, err)
	}
	audioFormat := binary.LittleEndian.Uint16(buffer[0:2])
	channels := binary.LittleEndian.Uint16(buffer[2:4])
	rate := binary.LittleEndian.Uint32(buffer[4:8])
	byteRate := binary.LittleEndian.Uint32(buffer[8:12])
	blockAlign := binary.LittleEndian.Uint16(buffer[12:14])
	bits := binary.LittleEndian.Uint16(buffer[14:16])
	validBits := bits
	channelMask := uint32(0)
	if audioFormat == formatExtensible {
		if size < 40 || len(buffer) < 40 || binary.LittleEndian.Uint16(buffer[16:18]) < 22 {
			return sample.Description{}, 0, fmt.Errorf("%w: extensible fmt chunk is incomplete", ErrMalformed)
		}
		validBits = binary.LittleEndian.Uint16(buffer[18:20])
		channelMask = binary.LittleEndian.Uint32(buffer[20:24])
		subFormat := buffer[24:40]
		if binary.LittleEndian.Uint16(subFormat[0:2]) != formatPCM || subFormat[2] != 0 || subFormat[3] != 0 || !bytes.Equal(subFormat[4:], extensibleBase[:]) {
			return sample.Description{}, 0, fmt.Errorf("%w: extensible subformat is not linear PCM", ErrUnsupported)
		}
		audioFormat = formatPCM
	}
	if audioFormat != formatPCM || bits != 16 || validBits == 0 || validBits > bits || rate == 0 {
		return sample.Description{}, 0, fmt.Errorf("%w: only 16-bit integer PCM is supported", ErrUnsupported)
	}
	var layout sample.Layout
	switch channels {
	case 1:
		layout = sample.Mono
		if channelMask != 0 && channelMask != 0x4 {
			return sample.Description{}, 0, fmt.Errorf("%w: mono channel mask is unsupported", ErrUnsupported)
		}
	case 2:
		layout = sample.Stereo
		if channelMask != 0 && channelMask != 0x3 {
			return sample.Description{}, 0, fmt.Errorf("%w: stereo channel mask is unsupported", ErrUnsupported)
		}
	default:
		return sample.Description{}, 0, fmt.Errorf("%w: channel count %d is unsupported", ErrUnsupported, channels)
	}
	expectedAlign := uint64(channels) * 2
	expectedRate := uint64(rate) * expectedAlign
	if uint64(blockAlign) != expectedAlign || expectedRate > math.MaxUint32 || uint64(byteRate) != expectedRate {
		return sample.Description{}, 0, fmt.Errorf("%w: PCM byte rate or block alignment is inconsistent", ErrMalformed)
	}
	description := sample.Description{
		Format:    sample.S16Interleaved,
		ValidBits: int(validBits),
		Rate:      int(rate),
		Layout:    layout,
		Endian:    sample.LittleEndian,
	}
	return description, int(blockAlign), nil
}

func checkedAdd(left, right uint64) (uint64, bool) {
	if left > math.MaxUint64-right {
		return 0, false
	}
	return left + right, true
}

// preserveBudget bounds what preserved chunks may allocate. A declared chunk
// size is content the source controls, so the allocation is refused before it
// is made rather than after a read fails.
type preserveBudget struct{ remaining uint64 }

func (b *preserveBudget) reserve(length uint64, id string) error {
	if length > b.remaining {
		return fmt.Errorf("%w: preserving chunk %q needs %d bytes and %d remain in the Inspect budget", ErrUnsupported, id, length, b.remaining)
	}
	b.remaining -= length
	return nil
}

// inspectTrailer preserves the bytes past the RIFF chunk as one opaque block.
// They are not a chunk, so they carry no header and are written back outside
// the RIFF size.
func inspectTrailer(ctx context.Context, reader access.Random, builder *metadata.Builder, budget *preserveBudget, start, end uint64) error {
	length := end - start
	if length > uint64(^uint(0)>>1) || start > math.MaxInt64 {
		return fmt.Errorf("%w: trailing region exceeds control-plane address space", ErrUnsupported)
	}
	if err := budget.reserve(length, "trailer"); err != nil {
		return err
	}
	raw := make([]byte, int(length))
	if err := access.ReadFullAt(ctx, reader, raw, int64(start)); err != nil {
		return fmt.Errorf("%w: trailing region at %d: %w", ErrMalformed, start, err)
	}
	builder.AddBlock(metadata.NewRawBlock(
		newChunkBlockID(start, chunkAfterRIFF, chunkRaw),
		rawChunkCarrier(),
		plugin.Identity{},
		metadata.NewBlob("application/octet-stream", raw),
	))
	return nil
}
