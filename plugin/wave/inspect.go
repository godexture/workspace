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
	"github.com/godexture/godec/resource"
)

const (
	// A page is also the largest source read performed by the range-preserving
	// mux. It is intentionally independent of the size of any source chunk.
	wavePageSize = 64 << 10
	// Semantic INFO values are useful control-plane data, while opaque source
	// bytes remain ranges. This cap bounds the temporary parser and rewrite
	// buffers even when the input has a very large LIST carrier.
	waveSemanticCap = 64 << 10
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
	return mediaformat.NewInspection(WAVE(), value), nil
}

func inspectHeader(ctx context.Context, reader access.Random) (header, error) {
	return inspectHeaderWithMetadata(ctx, reader, metadata.Resolver{})
}

func inspectHeaderWithMetadata(ctx context.Context, reader access.Random, resolver metadata.Resolver) (header, error) {
	return inspectHeaderWithSize(ctx, reader, 0, false, resolver, job.DefaultBudget().InspectMemory)
}

func inspectHeaderWithSize(ctx context.Context, reader access.Random, sourceSize uint64, sizeKnown bool, resolver metadata.Resolver, memoryLimit resource.Bytes) (header, error) {
	if reader == nil {
		return header{}, fmt.Errorf("%w: random reader is nil", ErrMalformed)
	}
	if !resolver.Valid() {
		resolver, _ = metadata.NewResolver(nil)
	}
	semanticLimit := uint64(memoryLimit)
	if semanticLimit > waveSemanticCap {
		semanticLimit = waveSemanticCap
	}
	var root [12]byte
	if err := access.ReadFullAt(ctx, reader, root[:], 0); err != nil {
		return header{}, fmt.Errorf("%w: RIFF header: %w", ErrMalformed, err)
	}
	rf64 := string(root[0:4]) == tagRF64
	if (string(root[0:4]) != tagRIFF && !rf64) || string(root[8:12]) != tagWAVE {
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
	result.rootEnd = rootEnd
	result.sourceSize = sourceSize

	document := metadata.NewBuilder(metadata.StreamScope)
	var formatFound, dataFound, ds64Found bool
	var ds64DataSize uint64
	offset := uint64(12)
	for {
		if err := context.Cause(ctx); err != nil {
			return header{}, err
		}
		// RF64's extended root end is unavailable until ds64 is seen. A
		// stable source size is still a hard scan boundary for malformed input.
		scanEnd := rootEnd
		if scanEnd == 0 && sizeKnown {
			scanEnd = sourceSize
		}
		if scanEnd != 0 {
			if offset == scanEnd {
				break
			}
			if offset > scanEnd || scanEnd-offset < 8 {
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
				result.rootEnd = rootEnd
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
			result.codecTag = CodecTag(description.Coding)
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
		if next <= offset || rootEnd != 0 && next > rootEnd || rootEnd == 0 && sizeKnown && next > sourceSize {
			return header{}, fmt.Errorf("%w: chunk exceeds RIFF bounds", ErrMalformed)
		}
		if preserve {
			value := sourceRange{offset: offset, length: next - offset}
			if err := result.ranges.add(anchor, value); err != nil {
				return header{}, fmt.Errorf("%w: non-contiguous WAVE preservation range", err)
			}
			if id == tagLIST && declaredSize >= 4 && uint64(declaredSize)+8+(declaredSize&1) == value.length && uint64(declaredSize)+8 <= semanticLimit {
				if err := inspectInfoSemantic(ctx, reader, resolver, document, &result.ranges, value, anchor); err != nil {
					return header{}, err
				}
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
		result.ranges.trailer = sourceRange{offset: rootEnd, length: sourceSize - rootEnd}
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

func inspectInfoSemantic(ctx context.Context, reader access.Random, resolver metadata.Resolver, builder *metadata.Builder, ranges *sourceRanges, value sourceRange, anchor chunkAnchor) error {
	if value.length > uint64(math.MaxInt) || value.offset > math.MaxInt64 {
		return fmt.Errorf("%w: LIST/INFO carrier exceeds runtime address space", ErrUnsupported)
	}
	raw := make([]byte, int(value.length))
	if err := access.ReadFullAt(ctx, reader, raw, int64(value.offset)); err != nil {
		return fmt.Errorf("%w: LIST/INFO carrier at %d: %w", ErrMalformed, value.offset, err)
	}
	// LIST is a generic RIFF carrier. Only its INFO subtype contributes
	// semantic metadata; every other subtype remains an opaque source range.
	if string(raw[8:12]) != tagINFO {
		return nil
	}
	block := newChunkBlockID(value.offset, anchor, chunkInfo)
	// Resolver.Parse is the binding/encoding validation boundary. Its result
	// is deliberately discarded because it contains source-backed Blobs.
	if _, err := resolver.Parse(ctx, RIFFInfo(), block, metadata.StreamScope, metadata.NewBlob("application/x-riff-info", raw)); err != nil {
		return err
	}
	semantic, err := parseInfoSemantic(raw)
	if err != nil {
		return err
	}
	if ranges.infoCount < 2 {
		ranges.infoCount++
	}
	if ranges.infoCount == 1 {
		ranges.info = value
	} else {
		ranges.info = sourceRange{}
	}
	builder.Append(semantic)
	return nil
}

// validateSourceEnd rejects a RIFF chunk that claims more than the source
// holds. Bytes past the chunk are not an error: appended tags and encoder
// padding are common, and the size field is the chunk boundary.
func validateSourceEnd(declared, actual uint64, known bool) error {
	if !known || declared <= actual {
		return nil
	}
	return fmt.Errorf("%w: RIFF ends at %d, source size is %d", ErrTruncatedData, declared, actual)
}

func inspectFormat(ctx context.Context, reader access.Random, offset, size uint64) (sample.Description, int, error) {
	if size < 16 {
		return sample.Description{}, 0, fmt.Errorf("%w: fmt chunk is shorter than 16 bytes", ErrMalformed)
	}
	buffer := make([]byte, int(min(size, 40)))
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
		if subFormat[2] != 0 || subFormat[3] != 0 || !bytes.Equal(subFormat[4:], extensibleBase[:]) {
			return sample.Description{}, 0, fmt.Errorf("%w: extensible subformat is not a linear PCM GUID", ErrUnsupported)
		}
		audioFormat = binary.LittleEndian.Uint16(subFormat[0:2])
	}
	coding := codingOf(audioFormat, int(bits))
	if !coding.Valid() || validBits == 0 || validBits > bits || rate == 0 {
		return sample.Description{}, 0, fmt.Errorf("%w: format %#04x at %d bits is not linear PCM this reader can express", ErrUnsupported, audioFormat, bits)
	}
	layout, ok := sample.FromMask(channelMask, int(channels))
	if !ok {
		return sample.Description{}, 0, fmt.Errorf("%w: channel mask %#x does not describe %d channels", ErrUnsupported, channelMask, channels)
	}
	if channelMask == 0 {
		layout = conventionalLayout(int(channels))
	}
	expectedAlign := uint64(channels) * uint64(coding.Bytes())
	expectedRate := uint64(rate) * expectedAlign
	if uint64(blockAlign) != expectedAlign || expectedRate > math.MaxUint32 || uint64(byteRate) != expectedRate {
		return sample.Description{}, 0, fmt.Errorf("%w: PCM byte rate or block alignment is inconsistent", ErrMalformed)
	}
	description := sample.Description{
		Coding:    coding,
		Packing:   sample.Interleaved,
		Endian:    sample.LittleEndian,
		Rate:      int(rate),
		Layout:    layout,
		ValidBits: int(validBits),
	}
	if coding.Bytes() == 1 {
		description.Endian = sample.NoEndian
	}
	if coding.Float() {
		description.ValidBits = coding.Bits()
	}
	if !description.Valid() {
		return sample.Description{}, 0, fmt.Errorf("%w: fmt chunk does not describe a usable stream", ErrUnsupported)
	}
	return description, int(blockAlign), nil
}

func checkedAdd(left, right uint64) (uint64, bool) {
	if left > math.MaxUint64-right {
		return 0, false
	}
	return left + right, true
}
