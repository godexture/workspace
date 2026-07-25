package dsp

import (
	"encoding/binary"
	"fmt"
)

// FromInt64 packs blockSize planar int64 samples per channel into
// interleaved PCM bytes, losslessly (no float32 roundtrip).
func FromInt64(dst []byte, src [][]int64, kind PCMKind, blockSize, channels int) error {
	switch kind {
	case PCMU8:
		packU8(dst, src, blockSize, channels)
	case PCMS8:
		packS8(dst, src, blockSize, channels)
	case PCMS16:
		packS16(dst, src, blockSize, channels)
	case PCMS24:
		packS24(dst, src, blockSize, channels)
	case PCMS32:
		packS32(dst, src, blockSize, channels)
	default:
		return fmt.Errorf("unsupported integer PCM kind: %d", kind)
	}
	return nil
}

func packU8(dst []byte, src [][]int64, blockSize, channels int) {
	bias := signedHalfRange(8)
	for sample := 0; sample < blockSize; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := sample*channels + ch
			dst[offset] = byte(src[ch][sample] + bias)
		}
	}
}

func packS8(dst []byte, src [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := sample*channels + ch
			dst[offset] = byte(int8(src[ch][sample]))
		}
	}
}

func packS16(dst []byte, src [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 2
			binary.LittleEndian.PutUint16(dst[offset:], uint16(int16(src[ch][sample])))
		}
	}
}

func packS24(dst []byte, src [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 3
			value := src[ch][sample]
			dst[offset] = byte(value)
			dst[offset+1] = byte(value >> 8)
			dst[offset+2] = byte(value >> 16)
		}
	}
}

func packS32Scalar(dst []byte, src [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for ch := 0; ch < channels; ch++ {
			offset := (sample*channels + ch) * 4
			binary.LittleEndian.PutUint32(dst[offset:], uint32(src[ch][sample]))
		}
	}
}

func packS32StereoScalar(dst []byte, left, right []int64, start, blockSize int) {
	for sample := start; sample < blockSize; sample++ {
		binary.LittleEndian.PutUint32(dst[sample*8:], uint32(left[sample]))
		binary.LittleEndian.PutUint32(dst[sample*8+4:], uint32(right[sample]))
	}
}
