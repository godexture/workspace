package pcm

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
)

// CreateAudioFrame creates a new AudioFrame containing the given float32 PCM samples converted to the target format.
func CreateAudioFrame(pcm []float32, attrs media.AudioAttributes) (*media.Frame, error) {
	channels := attrs.ChannelLayout.ChannelCount()
	if channels <= 0 {
		return nil, fmt.Errorf("invalid channel count: %d", channels)
	}
	if len(pcm)%channels != 0 {
		return nil, fmt.Errorf("PCM sample count %d is not divisible by %d channels", len(pcm), channels)
	}

	bitsPerSample := attrs.EffectiveBitsPerSample()
	storageBits := attrs.Format.BitsPerSample()
	if attrs.Format != media.SampleFormatF32 && (bitsPerSample <= 0 || bitsPerSample > storageBits) {
		return nil, fmt.Errorf("invalid %d-bit precision for %s storage", bitsPerSample, attrs.Format)
	}

	samples := len(pcm) / channels
	frameOpts := []media.AudioFrameOption(nil)
	if bitsPerSample > 0 {
		frameOpts = append(frameOpts, media.WithAudioBitsPerSample(bitsPerSample))
	}
	f := media.NewAudioFrame(attrs.Format, attrs.ChannelLayout, attrs.SampleRate, samples, frameOpts...)
	plane := f.Planes()[0]

	switch attrs.Format {
	case media.SampleFormatF32:
		for i, val := range pcm {
			binary.LittleEndian.PutUint32(plane[i*4:(i+1)*4], math.Float32bits(val))
		}
	case media.SampleFormatU8:
		for i, val := range pcm {
			scale := int64(1) << uint(bitsPerSample-1)
			value := signedPCMValue(val, bitsPerSample) + scale
			plane[i] = byte(value)
		}
	case media.SampleFormatS16:
		for i, val := range pcm {
			value := signedPCMValue(val, bitsPerSample)
			binary.LittleEndian.PutUint16(plane[i*2:(i+1)*2], uint16(int16(value)))
		}
	case media.SampleFormatS24:
		for i, val := range pcm {
			value := int32(signedPCMValue(val, bitsPerSample))
			offset := i * 3
			plane[offset] = byte(value)
			plane[offset+1] = byte(value >> 8)
			plane[offset+2] = byte(value >> 16)
		}
	case media.SampleFormatS32:
		for i, val := range pcm {
			value := signedPCMValue(val, bitsPerSample)
			binary.LittleEndian.PutUint32(plane[i*4:(i+1)*4], uint32(int32(value)))
		}
	default:
		return nil, fmt.Errorf("unsupported format for creation: %v", attrs.Format)
	}

	var frame media.Frame = f
	return &frame, nil
}

func signedPCMValue(value float32, bitsPerSample int) int64 {
	scale := int64(1) << uint(bitsPerSample-1)
	if value <= -1 {
		return -scale
	}
	if value >= 1 {
		return scale - 1
	}
	return int64(float64(value) * float64(scale))
}
