package media

import "strings"

type SampleFormat string

const (
	SampleFormatUnknown SampleFormat = ""

	// Interleaved
	SampleFormatU8  SampleFormat = "u8"  // Unsigned 8-bit
	SampleFormatS16 SampleFormat = "s16" // Signed 16-bit
	SampleFormatS32 SampleFormat = "s32" // Signed 32-bit
	SampleFormatF32 SampleFormat = "f32" // Float 32-bit
	SampleFormatF64 SampleFormat = "f64" // Double 64-bit

	// Planar
	SampleFormatU8P  SampleFormat = "u8p"  // Unsigned 8-bit
	SampleFormatS16P SampleFormat = "s16p" // Signed 16-bit
	SampleFormatS32P SampleFormat = "s32p" // Signed 32-bit
	SampleFormatF32P SampleFormat = "f32p" // Float 32-bit (recommended)
	SampleFormatF64P SampleFormat = "f64p" // Double 64-bit
)

func (f SampleFormat) IsPlanar() bool       { return strings.HasSuffix(string(f), "p") }
func (f SampleFormat) IsPacked() bool       { return !f.IsPlanar() }
func (f SampleFormat) Planar() SampleFormat { return f.Packed() + "p" }
func (f SampleFormat) Packed() SampleFormat { return SampleFormat(strings.TrimSuffix(string(f), "p")) }

func (f SampleFormat) BytesPerSample() int {
	switch f {
	case SampleFormatU8, SampleFormatU8P:
		return 1
	case SampleFormatS16, SampleFormatS16P:
		return 2
	case SampleFormatS32, SampleFormatF32, SampleFormatS32P, SampleFormatF32P:
		return 4
	case SampleFormatF64, SampleFormatF64P:
		return 8
	default:
		return 0
	}
}
