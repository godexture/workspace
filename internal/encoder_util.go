package internal

import (
	"encoding/binary"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/dsp"
)

// leftJustifyPCM shifts samples that occupy only the low BitsPerSample bits of
// their container format (e.g. 24-bit FLAC output carried in S24/S32 frames)
// up to full scale, which is what raw LPCM byte streams are expected to hold.
// scratch is reused (grown as needed) across calls instead of allocating a
// fresh output buffer every time; *scratch is left untouched when data is
// returned unchanged (the no-shift-needed path), so callers must not assume
// the result aliases *scratch.
func leftJustifyPCM(scratch *[]byte, data []byte, format media.SampleFormat, bitsPerSample int) []byte {
	containerBits := format.BitsPerSample()
	if bitsPerSample <= 0 || bitsPerSample >= containerBits {
		return data
	}
	shift := uint(containerBits - bitsPerSample)
	out := growBytes(*scratch, len(data))
	switch format {
	case media.SampleFormatS16:
		leftJustifyS16(out, data, shift)
	case media.SampleFormatS24:
		aligned := len(data) - len(data)%3
		for i := 0; i < aligned; i += 3 {
			v := (uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16) << shift
			out[i] = byte(v)
			out[i+1] = byte(v >> 8)
			out[i+2] = byte(v >> 16)
		}
		copy(out[aligned:], data[aligned:])
	case media.SampleFormatS32:
		leftJustifyS32(out, data, shift)
	default:
		return data
	}
	*scratch = out
	return out
}

// convertF32ToS16 reuses scratch (grown as needed) across calls instead of
// allocating a fresh output buffer every time.
func convertF32ToS16(scratch *[]byte, f32Data []byte) []byte {
	samples := len(f32Data) / 4
	s16Data := growBytes(*scratch, samples*2)
	*scratch = s16Data
	source := dsp.AsSamples[float32](f32Data)
	destination := dsp.AsSamples[int16](s16Data)
	if source != nil && destination != nil {
		dsp.ConvertF32ToS16(destination, source)
		return s16Data
	}
	for i := 0; i < samples; i++ {
		fBits := binary.LittleEndian.Uint32(f32Data[i*4 : i*4+4])
		fVal := math.Float32frombits(fBits)

		if fVal > 1.0 {
			fVal = 1.0
		} else if fVal < -1.0 {
			fVal = -1.0
		}

		var s16Val int16
		if fVal < 0 {
			s16Val = int16(fVal * 32768)
		} else {
			s16Val = int16(fVal * 32767)
		}
		binary.LittleEndian.PutUint16(s16Data[i*2:i*2+2], uint16(s16Val))
	}
	return s16Data
}

// growBytes returns dst resliced to length n if it already has the capacity,
// or a fresh []byte of length n otherwise.
func growBytes(dst []byte, n int) []byte {
	if cap(dst) < n {
		return make([]byte, n)
	}
	return dst[:n]
}
