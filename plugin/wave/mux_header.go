package wave

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/media/sample"
)

type muxHeader struct {
	initial   []byte
	afterData []byte
	trailer   []byte
	// rangeMode is used by an Inspection handoff. The fixed header pieces are
	// retained here; opaque source bytes are described by ranges and read only
	// by the Open-to-Close mux operator.
	rangeMode      bool
	prefix         []byte
	format         []byte
	dataTag        []byte
	ranges         sourceRanges
	replacement    sourceReplacement
	reserveOffset  int64
	dataSizeOffset int64
	dataOffset     int64
	blockAlign     uint64
}

type sourceReplacement struct {
	source  sourceRange
	payload []byte
}

type headerPatch struct {
	offset  int64
	payload []byte
}

type finalizedHeader struct {
	patches  []headerPatch
	padding  int
	fileSize uint64
	rf64     bool
}

func newMuxHeader(description sample.Description) (muxHeader, error) {
	return newMuxHeaderWithChunks(description, muxChunks{})
}

func newRangeMuxHeader(description sample.Description, inspected header) (muxHeader, error) {
	formatPayload, blockAlign, err := marshalFormat(description)
	if err != nil {
		return muxHeader{}, err
	}
	format := make([]byte, 8+len(formatPayload))
	copy(format[0:4], tagFMT)
	binary.LittleEndian.PutUint32(format[4:8], uint32(len(formatPayload)))
	copy(format[8:], formatPayload)
	prefix := make([]byte, reserveOffset)
	copy(prefix[0:4], tagRIFF)
	copy(prefix[8:12], tagWAVE)
	dataTag := make([]byte, 8)
	copy(dataTag, tagDATA)
	beforeFormat := inspected.ranges.beforeFormat.length
	beforeData := inspected.ranges.beforeData.length
	dataOffset := uint64(len(prefix) + 8 + ds64PayloadSize + len(format) + len(dataTag))
	if beforeFormat > math.MaxUint64-dataOffset || beforeData > math.MaxUint64-dataOffset-beforeFormat {
		return muxHeader{}, fmt.Errorf("%w: WAVE source ranges exceed output offsets", ErrUnsupported)
	}
	dataOffset += beforeFormat + beforeData
	if dataOffset > math.MaxInt64 {
		return muxHeader{}, fmt.Errorf("%w: WAVE source ranges exceed runtime offsets", ErrUnsupported)
	}
	return muxHeader{
		initial:        prefix,
		rangeMode:      true,
		prefix:         prefix,
		format:         format,
		dataTag:        dataTag,
		ranges:         inspected.ranges,
		reserveOffset:  reserveOffset,
		dataSizeOffset: int64(dataOffset - 4),
		dataOffset:     int64(dataOffset),
		blockAlign:     uint64(blockAlign),
	}, nil
}

func newMuxHeaderWithChunks(description sample.Description, chunks muxChunks) (muxHeader, error) {
	formatPayload, blockAlign, err := marshalFormat(description)
	if err != nil {
		return muxHeader{}, err
	}
	if len(chunks.beforeFormat) > math.MaxInt-80-len(formatPayload) || len(chunks.beforeData) > math.MaxInt-80-len(formatPayload)-len(chunks.beforeFormat) {
		return muxHeader{}, fmt.Errorf("%w: WAVE metadata header exceeds runtime address space", ErrUnsupported)
	}
	headerSize := reserveOffset + 8 + ds64PayloadSize + len(chunks.beforeFormat) + 8 + len(formatPayload) + len(chunks.beforeData) + 8
	value := make([]byte, headerSize)
	copy(value[0:4], tagRIFF)
	copy(value[8:12], tagWAVE)
	copy(value[reserveOffset:], reserveChunkOf(chunks))
	formatOffset := reserveOffset + 8 + ds64PayloadSize
	copy(value[formatOffset:], chunks.beforeFormat)
	formatOffset += len(chunks.beforeFormat)
	copy(value[formatOffset:formatOffset+4], tagFMT)
	binary.LittleEndian.PutUint32(value[formatOffset+4:formatOffset+8], uint32(len(formatPayload)))
	copy(value[formatOffset+8:], formatPayload)
	dataOffset := formatOffset + 8 + len(formatPayload)
	copy(value[dataOffset:], chunks.beforeData)
	dataOffset += len(chunks.beforeData)
	copy(value[dataOffset:dataOffset+4], tagDATA)
	return muxHeader{
		initial:        value,
		afterData:      append([]byte(nil), chunks.afterData...),
		trailer:        append([]byte(nil), chunks.trailer...),
		reserveOffset:  int64(reserveOffset),
		dataSizeOffset: int64(dataOffset + 4),
		dataOffset:     int64(headerSize),
		blockAlign:     uint64(blockAlign),
	}, nil
}

// reserveChunkOf writes the input's own reservation chunk back into the slot
// it came from, so a RIFF input that carried a non-zero JUNK there round-trips
// byte for byte. A file that needs RF64 loses those bytes: ds64 must occupy
// this slot, and the header layout is fixed before the data size is known, so
// there is nowhere else to keep them without moving every following chunk.
func reserveChunkOf(chunks muxChunks) []byte {
	if len(chunks.reservation) != 0 {
		return chunks.reservation
	}
	value := make([]byte, 8+ds64PayloadSize)
	copy(value[0:4], tagJUNK)
	binary.LittleEndian.PutUint32(value[4:8], ds64PayloadSize)
	return value
}

func marshalFormat(description sample.Description) ([]byte, int, error) {
	if !description.Valid() || description.Format != sample.S16Interleaved || description.Endian != sample.LittleEndian {
		return nil, 0, fmt.Errorf("%w: WAVE mux requires interleaved little-endian signed 16-bit PCM", ErrUnsupported)
	}
	channels := description.Layout.Channels()
	if channels < 1 || channels > 2 || description.Rate > math.MaxUint32 {
		return nil, 0, fmt.Errorf("%w: WAVE channel layout or sample rate is unsupported", ErrUnsupported)
	}
	blockAlign := channels * 2
	byteRate := uint64(description.Rate) * uint64(blockAlign)
	if byteRate > math.MaxUint32 {
		return nil, 0, fmt.Errorf("%w: WAVE byte rate exceeds 32 bits", ErrUnsupported)
	}
	size := 16
	if description.ValidBits != 16 {
		size = 40
	}
	value := make([]byte, size)
	formatTag := formatPCM
	if size == 40 {
		formatTag = formatExtensible
	}
	binary.LittleEndian.PutUint16(value[0:2], formatTag)
	binary.LittleEndian.PutUint16(value[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(value[4:8], uint32(description.Rate))
	binary.LittleEndian.PutUint32(value[8:12], uint32(byteRate))
	binary.LittleEndian.PutUint16(value[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(value[14:16], 16)
	if size == 40 {
		binary.LittleEndian.PutUint16(value[16:18], 22)
		binary.LittleEndian.PutUint16(value[18:20], uint16(description.ValidBits))
		mask := uint32(0x4)
		if description.Layout == sample.Stereo {
			mask = 0x3
		}
		binary.LittleEndian.PutUint32(value[20:24], mask)
		binary.LittleEndian.PutUint16(value[24:26], formatPCM)
		copy(value[28:40], extensibleBase[:])
	}
	return value, blockAlign, nil
}

func (h muxHeader) finalize(dataSize uint64) (finalizedHeader, error) {
	padding, fileSize, err := h.outputSize(dataSize)
	if err != nil {
		return finalizedHeader{}, err
	}
	riffSize := fileSize - 8
	rf64 := riffSize > math.MaxUint32 || dataSize > math.MaxUint32
	result := finalizedHeader{padding: padding, fileSize: fileSize, rf64: rf64}
	if !rf64 {
		root := make([]byte, 4)
		binary.LittleEndian.PutUint32(root, uint32(riffSize))
		data := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, uint32(dataSize))
		result.patches = []headerPatch{{offset: 4, payload: root}, {offset: h.dataSizeOffset, payload: data}}
		return result, nil
	}
	root := make([]byte, 8)
	copy(root[0:4], tagRF64)
	binary.LittleEndian.PutUint32(root[4:8], math.MaxUint32)
	ds64 := make([]byte, 8+ds64PayloadSize)
	copy(ds64[0:4], tagDS64)
	binary.LittleEndian.PutUint32(ds64[4:8], ds64PayloadSize)
	binary.LittleEndian.PutUint64(ds64[8:16], riffSize)
	binary.LittleEndian.PutUint64(ds64[16:24], dataSize)
	binary.LittleEndian.PutUint64(ds64[24:32], dataSize/h.blockAlign)
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, math.MaxUint32)
	result.patches = []headerPatch{
		{offset: 0, payload: root},
		{offset: h.reserveOffset, payload: ds64},
		{offset: h.dataSizeOffset, payload: data},
	}
	return result, nil
}

func (h muxHeader) outputSize(dataSize uint64) (int, uint64, error) {
	if h.rangeMode {
		return h.rangeOutputSize(dataSize)
	}
	if len(h.initial) == 0 || h.reserveOffset < 0 || h.dataSizeOffset < 0 || h.dataOffset != int64(len(h.initial)) || h.blockAlign == 0 {
		return 0, 0, fmt.Errorf("%w: WAVE mux header layout is invalid", ErrMalformed)
	}
	padding := dataSize & 1
	suffix := uint64(len(h.afterData))
	if dataSize > math.MaxUint64-padding || dataSize+padding > math.MaxUint64-suffix || uint64(h.dataOffset) > math.MaxUint64-dataSize-padding-suffix {
		return 0, 0, fmt.Errorf("%w: WAVE output size overflows", ErrUnsupported)
	}
	// riffEnd is where the RIFF chunk stops. A preserved trailing region sits
	// past it and never counts toward the RIFF or ds64 size.
	riffEnd := uint64(h.dataOffset) + dataSize + padding + suffix
	trailer := uint64(len(h.trailer))
	if riffEnd > math.MaxInt64 || riffEnd > math.MaxUint64-trailer || riffEnd+trailer > math.MaxInt64 {
		return 0, 0, fmt.Errorf("%w: WAVE output exceeds runtime offsets", ErrUnsupported)
	}
	return int(padding), riffEnd, nil
}

// payloadBytes is the largest single buffer the muxer emits from its own
// grant: the header, the region after the data chunk, or the trailing region.
func (h muxHeader) payloadBytes() int {
	if h.rangeMode {
		return max(len(h.prefix), len(h.format), len(h.dataTag), 8+ds64PayloadSize)
	}
	return max(len(h.initial), len(h.afterData), len(h.trailer))
}

func (h muxHeader) sourceEnd() uint64 {
	end := uint64(0)
	for _, value := range []sourceRange{h.ranges.reservation, h.ranges.beforeFormat, h.ranges.beforeData, h.ranges.afterData, h.ranges.trailer} {
		if valueEnd, ok := value.end(); ok && valueEnd > end {
			end = valueEnd
		}
	}
	return end
}

func (h muxHeader) rangeOutputSize(dataSize uint64) (int, uint64, error) {
	if len(h.prefix) == 0 || len(h.format) == 0 || len(h.dataTag) != 8 || h.reserveOffset < 0 || h.dataSizeOffset < 0 || h.dataOffset < 0 || h.blockAlign == 0 {
		return 0, 0, fmt.Errorf("%w: WAVE range mux header layout is invalid", ErrMalformed)
	}
	beforeFormat, beforeData, afterData, trailer, ok := h.outputRangeLengths()
	if !ok {
		return 0, 0, fmt.Errorf("%w: WAVE source range lengths overflow", ErrUnsupported)
	}
	base := uint64(len(h.prefix)) + uint64(8+ds64PayloadSize) + uint64(len(h.format)) + uint64(len(h.dataTag))
	if beforeFormat > math.MaxUint64-base || beforeData > math.MaxUint64-base-beforeFormat {
		return 0, 0, fmt.Errorf("%w: WAVE header size overflows", ErrUnsupported)
	}
	computedDataOffset := base + beforeFormat + beforeData
	if computedDataOffset > math.MaxInt64 || int64(computedDataOffset) != h.dataOffset {
		return 0, 0, fmt.Errorf("%w: WAVE range mux data offset changed unexpectedly", ErrMalformed)
	}
	padding := dataSize & 1
	if dataSize > math.MaxUint64-padding || dataSize+padding > math.MaxUint64-afterData || computedDataOffset > math.MaxUint64-dataSize-padding-afterData {
		return 0, 0, fmt.Errorf("%w: WAVE output size overflows", ErrUnsupported)
	}
	riffEnd := computedDataOffset + dataSize + padding + afterData
	if riffEnd > math.MaxInt64 || riffEnd > math.MaxUint64-trailer || riffEnd+trailer > math.MaxInt64 {
		return 0, 0, fmt.Errorf("%w: WAVE output exceeds runtime offsets", ErrUnsupported)
	}
	return int(padding), riffEnd, nil
}

func (h muxHeader) outputRangeLengths() (beforeFormat, beforeData, afterData, trailer uint64, ok bool) {
	beforeFormat = h.ranges.beforeFormat.length
	beforeData = h.ranges.beforeData.length
	afterData = h.ranges.afterData.length
	trailer = h.ranges.trailer.length
	if !h.replacement.source.valid() {
		return beforeFormat, beforeData, afterData, trailer, true
	}
	old := h.replacement.source.length
	newLength := uint64(len(h.replacement.payload))
	if old > newLength {
		shrink := old - newLength
		switch {
		case h.replacement.source.offset >= h.ranges.beforeFormat.offset && h.replacement.source.offset < h.ranges.beforeFormat.offset+h.ranges.beforeFormat.length:
			if shrink > beforeFormat {
				return 0, 0, 0, 0, false
			}
			beforeFormat -= shrink
		case h.replacement.source.offset >= h.ranges.beforeData.offset && h.replacement.source.offset < h.ranges.beforeData.offset+h.ranges.beforeData.length:
			if shrink > beforeData {
				return 0, 0, 0, 0, false
			}
			beforeData -= shrink
		case h.replacement.source.offset >= h.ranges.afterData.offset && h.replacement.source.offset < h.ranges.afterData.offset+h.ranges.afterData.length:
			if shrink > afterData {
				return 0, 0, 0, 0, false
			}
			afterData -= shrink
		default:
			return 0, 0, 0, 0, false
		}
		return beforeFormat, beforeData, afterData, trailer, true
	}
	grow := newLength - old
	switch {
	case h.replacement.source.offset >= h.ranges.beforeFormat.offset && h.replacement.source.offset < h.ranges.beforeFormat.offset+h.ranges.beforeFormat.length:
		if beforeFormat > math.MaxUint64-grow {
			return 0, 0, 0, 0, false
		}
		beforeFormat += grow
	case h.replacement.source.offset >= h.ranges.beforeData.offset && h.replacement.source.offset < h.ranges.beforeData.offset+h.ranges.beforeData.length:
		if beforeData > math.MaxUint64-grow {
			return 0, 0, 0, 0, false
		}
		beforeData += grow
	case h.replacement.source.offset >= h.ranges.afterData.offset && h.replacement.source.offset < h.ranges.afterData.offset+h.ranges.afterData.length:
		if afterData > math.MaxUint64-grow {
			return 0, 0, 0, 0, false
		}
		afterData += grow
	default:
		return 0, 0, 0, 0, false
	}
	return beforeFormat, beforeData, afterData, trailer, true
}

func (h *muxHeader) applyReplacement(value sourceReplacement) error {
	if h == nil || !h.rangeMode || !value.source.valid() {
		return fmt.Errorf("%w: WAVE source replacement is invalid", ErrMalformed)
	}
	h.replacement = value
	beforeFormat, beforeData, _, _, ok := h.outputRangeLengths()
	if !ok {
		return fmt.Errorf("%w: WAVE replacement length overflows", ErrUnsupported)
	}
	dataOffset := uint64(len(h.prefix)) + uint64(8+ds64PayloadSize) + uint64(len(h.format)) + uint64(len(h.dataTag))
	if beforeFormat > math.MaxUint64-dataOffset || beforeData > math.MaxUint64-dataOffset-beforeFormat {
		return fmt.Errorf("%w: WAVE replacement data offset overflows", ErrUnsupported)
	}
	dataOffset += beforeFormat + beforeData
	if dataOffset > math.MaxInt64 {
		return fmt.Errorf("%w: WAVE replacement data offset exceeds runtime offsets", ErrUnsupported)
	}
	h.dataOffset = int64(dataOffset)
	h.dataSizeOffset = h.dataOffset - 4
	return nil
}
