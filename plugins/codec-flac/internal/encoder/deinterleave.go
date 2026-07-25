package encoder

import (
	"encoding/binary"
	"fmt"
)

func deinterleaveS8(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := sample*channels + ch
			value := int64(int8(plane[offset]))
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}

func deinterleaveS16(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 2
			value := int64(int16(binary.LittleEndian.Uint16(plane[offset : offset+2])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}

func deinterleaveS24(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 3
			raw := int32(uint32(plane[offset]) | uint32(plane[offset+1])<<8 | uint32(plane[offset+2])<<16)
			if raw&0x800000 != 0 {
				raw |= ^int32(0xffffff)
			}
			value := int64(raw)
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}

func deinterleaveS32Scalar(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 4
			value := int64(int32(binary.LittleEndian.Uint32(plane[offset : offset+4])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("FLAC sample %d outside %d-bit range", value, bitsPerSample)
			}
			buffer[ch][writeStart+sample] = value
		}
	}
	return nil
}
