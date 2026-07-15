package pcm

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
)

// ConvertToFloat32 converts an AudioFrame's samples to a float32 slice.
func ConvertToFloat32(af *media.AudioFrame) ([]float32, error) {
	return convertToFloat32(nil, af)
}

func convertToFloat32(dst []float32, af *media.AudioFrame) ([]float32, error) {
	plane := af.Planes()[0]
	channels := af.Layout.ChannelCount()
	samples := af.Samples
	totalSamples := samples * channels
	bitsPerSample := af.BitsPerSample
	if bitsPerSample <= 0 {
		bitsPerSample = af.Format.BytesPerSample() * 8
	}

	if cap(dst) < totalSamples {
		dst = make([]float32, totalSamples)
	} else {
		dst = dst[:totalSamples]
	}
	switch af.Format {
	case media.SampleFormatU8:
		for i := 0; i < totalSamples; i++ {
			dst[i] = (float32(plane[i]) - 128.0) / 128.0
		}
	case media.SampleFormatS16:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			val := int16(binary.LittleEndian.Uint16(plane[i*2 : (i+1)*2]))
			dst[i] = float32(val) / scale
		}
	case media.SampleFormatS24:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			offset := i * 3
			value := int32(uint32(plane[offset]) | uint32(plane[offset+1])<<8 | uint32(plane[offset+2])<<16)
			if value&0x800000 != 0 {
				value |= ^int32(0xffffff)
			}
			dst[i] = float32(value) / scale
		}
	case media.SampleFormatS32:
		scale := float32(uint64(1) << uint(bitsPerSample-1))
		for i := 0; i < totalSamples; i++ {
			val := int32(binary.LittleEndian.Uint32(plane[i*4 : (i+1)*4]))
			dst[i] = float32(val) / scale
		}
	case media.SampleFormatF32:
		for i := 0; i < totalSamples; i++ {
			bits := binary.LittleEndian.Uint32(plane[i*4 : (i+1)*4])
			dst[i] = math.Float32frombits(bits)
		}
	default:
		return nil, fmt.Errorf("unsupported sample format in conversion: %v", af.Format)
	}
	return dst, nil
}
