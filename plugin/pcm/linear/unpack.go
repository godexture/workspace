package linear

import (
	"fmt"
	"unsafe"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
)

// unpack writes one channel of an interleaved wire block into a window of that
// channel's plane. Choosing the function is a Compile-time decision, so the
// sample loop never looks at the coding again.
type unpack[S audio.Sample] func(window []S, block []byte, channel, channels int)

// newUnpack returns the loop that reads description's wire coding, and the
// sample width whose bytes the caller normalizes to native order first.
func newUnpack[S audio.Sample](description sample.Description) (unpack[S], int, error) {
	if !sample.Stores[S](description.Coding) {
		return nil, 0, fmt.Errorf("%w: %s does not decode into %s frames", ErrUnsupportedCoding, description.Coding, sample.CodingOf[S]())
	}
	var erased any
	switch description.Coding {
	case sample.U8:
		erased = unpack[int16](unpackUnsigned8)
	case sample.S8:
		erased = unpack[int16](unpackSigned8)
	case sample.S24:
		if description.Endian == sample.BigEndian {
			erased = unpack[int32](unpackSigned24BigEndian)
		} else {
			erased = unpack[int32](unpackSigned24LittleEndian)
		}
	default:
		// The remaining codings are the canonical widths, so a native-order
		// block is already a slice of S.
		erased = unpack[S](unpackNative[S])
	}
	typed, ok := erased.(unpack[S])
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", ErrUnsupportedCoding, description.Coding)
	}
	return typed, swapWidth(description), nil
}

func unpackNative[S audio.Sample](window []S, block []byte, channel, channels int) {
	if len(window) == 0 {
		return
	}
	size := int(unsafe.Sizeof(*new(S)))
	values := unsafe.Slice((*S)(unsafe.Pointer(&block[0])), len(block)/size)
	index := channel
	for position := range window {
		window[position] = values[index]
		index += channels
	}
}

// unpackUnsigned8 recentres WAVE's unsigned 8-bit samples and left-aligns them
// in the 16-bit container, so a stored value always spans its coding's full
// scale and ValidBits stays a statement about the source rather than a shift.
func unpackUnsigned8(window []int16, block []byte, channel, channels int) {
	index := channel
	for position := range window {
		window[position] = (int16(block[index]) - 128) << 8
		index += channels
	}
}

func unpackSigned8(window []int16, block []byte, channel, channels int) {
	index := channel
	for position := range window {
		window[position] = int16(int8(block[index])) << 8
		index += channels
	}
}

func unpackSigned24LittleEndian(window []int32, block []byte, channel, channels int) {
	offset, stride := channel*3, channels*3
	for position := range window {
		window[position] = int32(uint32(block[offset])<<8 | uint32(block[offset+1])<<16 | uint32(block[offset+2])<<24)
		offset += stride
	}
}

func unpackSigned24BigEndian(window []int32, block []byte, channel, channels int) {
	offset, stride := channel*3, channels*3
	for position := range window {
		window[position] = int32(uint32(block[offset])<<24 | uint32(block[offset+1])<<16 | uint32(block[offset+2])<<8)
		offset += stride
	}
}
