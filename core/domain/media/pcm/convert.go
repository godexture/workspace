package pcm

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/dsp"
)

func ToFloat32(dst []float32, src []byte, format media.SampleFormat, bitsPerSample int) ([]float32, error) {
	return dsp.ToFloat32(dst, src, SampleKind(format), bitsPerSample)
}

func FromFloat32(dst []byte, src []float32, format media.SampleFormat, bitsPerSample int) error {
	return dsp.FromFloat32(dst, src, SampleKind(format), bitsPerSample)
}

// SampleKind maps a media.SampleFormat to the pkg/dsp PCM kind that encodes
// or decodes its raw bytes.
func SampleKind(format media.SampleFormat) dsp.PCMKind {
	switch format.Packed() {
	case media.SampleFormatU8:
		return dsp.PCMU8
	case media.SampleFormatS8:
		return dsp.PCMS8
	case media.SampleFormatS16:
		return dsp.PCMS16
	case media.SampleFormatS24:
		return dsp.PCMS24
	case media.SampleFormatS32:
		return dsp.PCMS32
	case media.SampleFormatF32:
		return dsp.PCMF32
	case media.SampleFormatF64:
		return dsp.PCMF64
	default:
		return dsp.PCMUnknown
	}
}

func ValidateFormat(format media.SampleFormat) error {
	if SampleKind(format) == dsp.PCMUnknown {
		return fmt.Errorf("unsupported sample format: %s", format)
	}
	return nil
}
