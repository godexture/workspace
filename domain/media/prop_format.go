package media

import "strings"

type SampleFormat string

const (
	SampleFormatUnknown SampleFormat = ""

	// Interleaved
	SampleFormatU8  SampleFormat = "u8"  // Unsigned 8-bit
	SampleFormatS8  SampleFormat = "s8"  // Signed 8-bit
	SampleFormatS16 SampleFormat = "s16" // Signed 16-bit
	SampleFormatS24 SampleFormat = "s24" // Signed 24-bit
	SampleFormatS32 SampleFormat = "s32" // Signed 32-bit
	SampleFormatF32 SampleFormat = "f32" // Float 32-bit
	SampleFormatF64 SampleFormat = "f64" // Double 64-bit

	// Planar
	SampleFormatU8P  SampleFormat = "u8p"  // Unsigned 8-bit
	SampleFormatS8P  SampleFormat = "s8p"  // Signed 8-bit
	SampleFormatS16P SampleFormat = "s16p" // Signed 16-bit
	SampleFormatS24P SampleFormat = "s24p" // Signed 24-bit
	SampleFormatS32P SampleFormat = "s32p" // Signed 32-bit
	SampleFormatF32P SampleFormat = "f32p" // Float 32-bit (recommended)
	SampleFormatF64P SampleFormat = "f64p" // Double 64-bit
)

func (f SampleFormat) IsPlanar() bool {
	return f == SampleFormatU8P || f == SampleFormatS8P || f == SampleFormatS16P || f == SampleFormatS24P || f == SampleFormatS32P || f == SampleFormatF32P || f == SampleFormatF64P
}

func (f SampleFormat) IsPacked() bool {
	return f == SampleFormatU8 || f == SampleFormatS8 || f == SampleFormatS16 || f == SampleFormatS24 || f == SampleFormatS32 || f == SampleFormatF32 || f == SampleFormatF64
}

func (f SampleFormat) IsInteger() bool {
	return f == SampleFormatU8 || f == SampleFormatS8 || f == SampleFormatS16 || f == SampleFormatS24 || f == SampleFormatS32 || f == SampleFormatU8P || f == SampleFormatS8P || f == SampleFormatS16P || f == SampleFormatS24P || f == SampleFormatS32P
}

func (f SampleFormat) IsFloat() bool {
	return f == SampleFormatF32 || f == SampleFormatF64 || f == SampleFormatF32P || f == SampleFormatF64P
}

func (f SampleFormat) Planar() SampleFormat { return f.Packed() + "p" }
func (f SampleFormat) Packed() SampleFormat { return SampleFormat(strings.TrimSuffix(string(f), "p")) }

func (f SampleFormat) BytesPerSample() int {
	switch f {
	case SampleFormatU8, SampleFormatU8P, SampleFormatS8, SampleFormatS8P:
		return 1
	case SampleFormatS16, SampleFormatS16P:
		return 2
	case SampleFormatS24, SampleFormatS24P:
		return 3
	case SampleFormatS32, SampleFormatF32, SampleFormatS32P, SampleFormatF32P:
		return 4
	case SampleFormatF64, SampleFormatF64P:
		return 8
	default:
		return 0
	}
}

// BitsPerSample returns the storage width of one sample in bits (e.g. 24
// for SampleFormatS24), regardless of packed/planar layout.
func (f SampleFormat) BitsPerSample() int {
	return f.BytesPerSample() * 8
}
