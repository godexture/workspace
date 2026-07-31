package dsp

import (
	"encoding/binary"
	"fmt"
)

// ToInt64 unpacks interleaved PCM bytes into planar int64 sample buffers,
// losslessly (no float32 roundtrip) and validating every decoded value
// against [minValue, maxValue]. dst[ch] must have room for at least
// writeStart+samples elements.
func ToInt64(dst [][]int64, src []byte, kind PCMKind, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	switch kind {
	case PCMU8:
		return unpackU8(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
	case PCMS8:
		return unpackS8(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
	case PCMS16:
		return unpackS16(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
	case PCMS24:
		return unpackS24(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
	case PCMS32:
		return unpackS32(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
	default:
		return fmt.Errorf("unsupported integer PCM kind: %d", kind)
	}
}

func unpackU8(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := sample*channels + ch
			value := int64(src[offset]) - signedHalfRange(8)
			if value < minValue || value > maxValue {
				return fmt.Errorf("PCM sample %d outside %d-bit range", value, bitsPerSample)
			}
			dst[ch][writeStart+sample] = value
		}
	}
	return nil
}

func unpackS8(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := sample*channels + ch
			value := int64(int8(src[offset]))
			if value < minValue || value > maxValue {
				return fmt.Errorf("PCM sample %d outside %d-bit range", value, bitsPerSample)
			}
			dst[ch][writeStart+sample] = value
		}
	}
	return nil
}

func unpackS16(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 2
			value := int64(int16(binary.LittleEndian.Uint16(src[offset : offset+2])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("PCM sample %d outside %d-bit range", value, bitsPerSample)
			}
			dst[ch][writeStart+sample] = value
		}
	}
	return nil
}

func unpackS24(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 3
			raw := int32(uint32(src[offset]) | uint32(src[offset+1])<<8 | uint32(src[offset+2])<<16)
			if raw&0x800000 != 0 {
				raw |= ^int32(0xffffff)
			}
			value := int64(raw)
			if value < minValue || value > maxValue {
				return fmt.Errorf("PCM sample %d outside %d-bit range", value, bitsPerSample)
			}
			dst[ch][writeStart+sample] = value
		}
	}
	return nil
}

func unpackS32Scalar(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	for sample := 0; sample < samples; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 4
			value := int64(int32(binary.LittleEndian.Uint32(src[offset : offset+4])))
			if value < minValue || value > maxValue {
				return fmt.Errorf("PCM sample %d outside %d-bit range", value, bitsPerSample)
			}
			dst[ch][writeStart+sample] = value
		}
	}
	return nil
}
