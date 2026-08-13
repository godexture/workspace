package linear

import (
	"encoding/binary"
	"math"
	"unsafe"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
)

const pcmBlockBytes = 4 * 1024

func decodePCM(encoded buffer.Bytes, scratch []byte, destinations [2][]byte, channels, samples int, shift uint, littleEndian bool) error {
	if channels < 1 || channels > len(destinations) || samples < 0 {
		return ErrPlaneCount
	}
	frameBytes := channels * 2
	block := len(scratch) - len(scratch)%frameBytes
	if block == 0 || samples > math.MaxInt/frameBytes || !encoded.Valid() || encoded.Len() < samples*frameBytes {
		return ErrPartialSample
	}
	for channel := 0; channel < channels; channel++ {
		if samples > math.MaxInt/2 || len(destinations[channel]) < samples*2 {
			return audio.ErrInvalidPlanes
		}
	}
	frames, err := encoded.Slice(0, samples*frameBytes)
	if err != nil {
		return err
	}
	// Trimming the block and windowing each destination keeps the interleave
	// loop free of bounds checks the caller-owned scratch would otherwise cost.
	return frames.Blocks(scratch[:block], func(part []byte, offset int) error {
		first, count := offset/frameBytes, len(part)/frameBytes
		part = part[:count*frameBytes]
		for channel := 0; channel < channels; channel++ {
			destination := destinations[channel][first*2 : (first+count)*2]
			position := channel * 2
			for index := 0; index < count; index++ {
				value := int16(wireUint16(part, position, littleEndian)) >> shift
				binary.NativeEndian.PutUint16(destination[index*2:], uint16(value))
				position += frameBytes
			}
		}
		return nil
	})
}

// encodePCM drains each plane through a caller-owned int16 scratch so the
// interleave loop keeps a native sample load. Blocks fills the scratch through
// its byte alias, which is why the block is safe to retype here.
func encodePCM(frame audio.Frame[int16], scratch []int16, encoded []byte, channels int, shift uint, littleEndian bool) error {
	if channels < 1 || channels > 2 || !frame.Valid() {
		return ErrPlaneCount
	}
	samples := frame.Samples()
	if len(scratch) == 0 || samples > math.MaxInt/(channels*2) || len(encoded) < samples*channels*2 {
		return ErrPartialSample
	}
	block := unsafe.Slice((*byte)(unsafe.Pointer(&scratch[0])), len(scratch)*2)
	for channel := 0; channel < channels; channel++ {
		plane, err := frame.Plane(channel)
		if err != nil {
			return err
		}
		occupied, err := plane.Slice(0, samples*2)
		if err != nil {
			return err
		}
		err = occupied.Blocks(block, func(part []byte, offset int) error {
			first := offset / 2
			for index, value := range unsafe.Slice((*int16)(unsafe.Pointer(&part[0])), len(part)/2) {
				putWireUint16(encoded, ((first+index)*channels+channel)*2, uint16(value)<<shift, littleEndian)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func wireUint16(data []byte, offset int, littleEndian bool) uint16 {
	if littleEndian {
		return uint16(data[offset]) | uint16(data[offset+1])<<8
	}
	return uint16(data[offset])<<8 | uint16(data[offset+1])
}

func putWireUint16(data []byte, offset int, value uint16, littleEndian bool) {
	if littleEndian {
		data[offset], data[offset+1] = byte(value), byte(value>>8)
		return
	}
	data[offset], data[offset+1] = byte(value>>8), byte(value)
}
