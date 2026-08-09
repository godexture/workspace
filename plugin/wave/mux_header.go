package wave

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/media/sample"
)

const ds64PayloadSize = 28

type muxHeader struct {
	initial        []byte
	reserveOffset  int64
	dataSizeOffset int64
	dataOffset     int64
	blockAlign     uint64
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
	formatPayload, blockAlign, err := marshalFormat(description)
	if err != nil {
		return muxHeader{}, err
	}
	headerSize := 12 + 8 + ds64PayloadSize + 8 + len(formatPayload) + 8
	value := make([]byte, headerSize)
	copy(value[0:4], tagRIFF)
	copy(value[8:12], tagWAVE)
	reserveOffset := 12
	copy(value[reserveOffset:reserveOffset+4], "JUNK")
	binary.LittleEndian.PutUint32(value[reserveOffset+4:reserveOffset+8], ds64PayloadSize)
	formatOffset := reserveOffset + 8 + ds64PayloadSize
	copy(value[formatOffset:formatOffset+4], tagFMT)
	binary.LittleEndian.PutUint32(value[formatOffset+4:formatOffset+8], uint32(len(formatPayload)))
	copy(value[formatOffset+8:], formatPayload)
	dataOffset := formatOffset + 8 + len(formatPayload)
	copy(value[dataOffset:dataOffset+4], tagDATA)
	return muxHeader{
		initial:        value,
		reserveOffset:  int64(reserveOffset),
		dataSizeOffset: int64(dataOffset + 4),
		dataOffset:     int64(headerSize),
		blockAlign:     uint64(blockAlign),
	}, nil
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
	if len(h.initial) == 0 || h.reserveOffset < 0 || h.dataSizeOffset < 0 || h.dataOffset != int64(len(h.initial)) || h.blockAlign == 0 {
		return 0, 0, fmt.Errorf("%w: WAVE mux header layout is invalid", ErrMalformed)
	}
	padding := dataSize & 1
	if dataSize > math.MaxUint64-padding || uint64(h.dataOffset) > math.MaxUint64-dataSize-padding {
		return 0, 0, fmt.Errorf("%w: WAVE output size overflows", ErrUnsupported)
	}
	fileSize := uint64(h.dataOffset) + dataSize + padding
	if fileSize > math.MaxInt64 {
		return 0, 0, fmt.Errorf("%w: WAVE output exceeds runtime offsets", ErrUnsupported)
	}
	return int(padding), fileSize, nil
}
