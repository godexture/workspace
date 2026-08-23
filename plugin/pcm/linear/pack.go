package linear

import (
	"fmt"
	"unsafe"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
)

// pack writes one channel's plane window into an interleaved wire block. Like
// unpack it is chosen once, so the sample loop never looks at the coding.
type pack[S audio.Sample] func(block []byte, window []S, channel, channels int)

// newPack returns the loop that writes description's wire coding, and the
// sample width whose bytes the caller normalizes afterwards.
func newPack[S audio.Sample](description sample.Description) (pack[S], int, error) {
	if !sample.Stores[S](description.Coding) {
		return nil, 0, fmt.Errorf("%w: %s cannot be written from %s frames", ErrUnsupportedCoding, description.Coding, sample.CodingOf[S]())
	}
	var erased any
	switch description.Coding {
	case sample.U8:
		erased = pack[int16](packUnsigned8)
	case sample.S8:
		erased = pack[int16](packSigned8)
	case sample.S24:
		if description.Endian == sample.BigEndian {
			erased = pack[int32](packSigned24BigEndian)
		} else {
			erased = pack[int32](packSigned24LittleEndian)
		}
	default:
		erased = pack[S](packNative[S])
	}
	typed, ok := erased.(pack[S])
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", ErrUnsupportedCoding, description.Coding)
	}
	return typed, swapWidth(description), nil
}

func packNative[S audio.Sample](block []byte, window []S, channel, channels int) {
	if len(window) == 0 {
		return
	}
	size := int(unsafe.Sizeof(*new(S)))
	values := unsafe.Slice((*S)(unsafe.Pointer(&block[0])), len(block)/size)
	index := channel
	for position := range window {
		values[index] = window[position]
		index += channels
	}
}

func packUnsigned8(block []byte, window []int16, channel, channels int) {
	index := channel
	for position := range window {
		block[index] = byte((window[position] >> 8) + 128)
		index += channels
	}
}

func packSigned8(block []byte, window []int16, channel, channels int) {
	index := channel
	for position := range window {
		block[index] = byte(window[position] >> 8)
		index += channels
	}
}

func packSigned24LittleEndian(block []byte, window []int32, channel, channels int) {
	offset, stride := channel*3, channels*3
	for position := range window {
		value := uint32(window[position])
		block[offset], block[offset+1], block[offset+2] = byte(value>>8), byte(value>>16), byte(value>>24)
		offset += stride
	}
}

func packSigned24BigEndian(block []byte, window []int32, channel, channels int) {
	offset, stride := channel*3, channels*3
	for position := range window {
		value := uint32(window[position])
		block[offset], block[offset+1], block[offset+2] = byte(value>>24), byte(value>>16), byte(value>>8)
		offset += stride
	}
}
