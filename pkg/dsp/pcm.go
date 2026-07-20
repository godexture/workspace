package dsp

import (
	"encoding/binary"
	"fmt"
	"math"
)

type PCMKind uint8

const (
	PCMUnknown PCMKind = iota
	PCMU8
	PCMS16
	PCMS24
	PCMS32
	PCMF32
	PCMF64
)

func (k PCMKind) BytesPerSample() int {
	switch k {
	case PCMU8:
		return 1
	case PCMS16:
		return 2
	case PCMS24:
		return 3
	case PCMS32, PCMF32:
		return 4
	case PCMF64:
		return 8
	default:
		return 0
	}
}

func ToFloat32(dst []float32, src []byte, kind PCMKind, bitsPerSample int) ([]float32, error) {
	bytesPerSample := kind.BytesPerSample()
	if bytesPerSample == 0 {
		return nil, fmt.Errorf("unsupported PCM kind: %d", kind)
	}
	if len(src)%bytesPerSample != 0 {
		return nil, fmt.Errorf("PCM data length %d is not divisible by sample width %d", len(src), bytesPerSample)
	}
	bitsPerSample, err := resolveBits(kind, bitsPerSample)
	if err != nil {
		return nil, err
	}
	samples := len(src) / bytesPerSample
	if cap(dst) < samples {
		dst = make([]float32, samples)
	} else {
		dst = dst[:samples]
	}

	switch kind {
	case PCMU8:
		scale := float32(signedHalfRange(bitsPerSample))
		midpoint := scale
		for i, value := range src {
			dst[i] = (float32(value) - midpoint) / scale
		}
	case PCMS16:
		scale := float32(signedHalfRange(bitsPerSample))
		for i := range dst {
			dst[i] = float32(int16(binary.LittleEndian.Uint16(src[i*2:]))) / scale
		}
	case PCMS24:
		scale := float32(signedHalfRange(bitsPerSample))
		for i := range dst {
			offset := i * 3
			value := int32(uint32(src[offset]) | uint32(src[offset+1])<<8 | uint32(src[offset+2])<<16)
			if value&0x800000 != 0 {
				value |= ^int32(0xffffff)
			}
			dst[i] = float32(value) / scale
		}
	case PCMS32:
		scale := float32(signedHalfRange(bitsPerSample))
		for i := range dst {
			dst[i] = float32(int32(binary.LittleEndian.Uint32(src[i*4:]))) / scale
		}
	case PCMF32:
		for i := range dst {
			dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(src[i*4:]))
		}
	case PCMF64:
		for i := range dst {
			dst[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(src[i*8:])))
		}
	}
	return dst, nil
}

func FromFloat32(dst []byte, src []float32, kind PCMKind, bitsPerSample int) error {
	bytesPerSample := kind.BytesPerSample()
	if bytesPerSample == 0 {
		return fmt.Errorf("unsupported PCM kind: %d", kind)
	}
	if len(dst) != len(src)*bytesPerSample {
		return fmt.Errorf("PCM destination has %d bytes, want %d", len(dst), len(src)*bytesPerSample)
	}
	bitsPerSample, err := resolveBits(kind, bitsPerSample)
	if err != nil {
		return err
	}

	switch kind {
	case PCMU8:
		max := int64((uint64(1) << uint(bitsPerSample)) - 1)
		midpoint := signedHalfRange(bitsPerSample)
		for i, sample := range src {
			value := midpoint + scaleSignedSample(sample, bitsPerSample)
			if value < 0 {
				value = 0
			} else if value > max {
				value = max
			}
			dst[i] = byte(value)
		}
	case PCMS16:
		for i, sample := range src {
			binary.LittleEndian.PutUint16(dst[i*2:], uint16(int16(scaleSignedSample(sample, bitsPerSample))))
		}
	case PCMS24:
		for i, sample := range src {
			value := uint32(int32(scaleSignedSample(sample, bitsPerSample)))
			offset := i * 3
			dst[offset] = byte(value)
			dst[offset+1] = byte(value >> 8)
			dst[offset+2] = byte(value >> 16)
		}
	case PCMS32:
		for i, sample := range src {
			binary.LittleEndian.PutUint32(dst[i*4:], uint32(int32(scaleSignedSample(sample, bitsPerSample))))
		}
	case PCMF32:
		for i, sample := range src {
			binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(sample))
		}
	case PCMF64:
		for i, sample := range src {
			binary.LittleEndian.PutUint64(dst[i*8:], math.Float64bits(float64(sample)))
		}
	}
	return nil
}

func resolveBits(kind PCMKind, bitsPerSample int) (int, error) {
	if kind == PCMF32 || kind == PCMF64 {
		return kind.BytesPerSample() * 8, nil
	}
	containerBits := kind.BytesPerSample() * 8
	if bitsPerSample == 0 {
		return containerBits, nil
	}
	if bitsPerSample < 1 || bitsPerSample > containerBits {
		return 0, fmt.Errorf("invalid %d-bit PCM width for %d-bit container", bitsPerSample, containerBits)
	}
	return bitsPerSample, nil
}

func scaleSignedSample(sample float32, bitsPerSample int) int64 {
	if math.IsNaN(float64(sample)) {
		return 0
	}
	negativeScale := signedHalfRange(bitsPerSample)
	positiveScale := negativeScale - 1
	if sample <= -1 || math.IsInf(float64(sample), -1) {
		return -negativeScale
	}
	if sample >= 1 || math.IsInf(float64(sample), 1) {
		return positiveScale
	}
	if sample < 0 {
		return int64(sample * float32(negativeScale))
	}
	return int64(sample * float32(positiveScale))
}

func signedHalfRange(bitsPerSample int) int64 {
	return int64(uint64(1) << uint(bitsPerSample-1))
}
