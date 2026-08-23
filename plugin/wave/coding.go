package wave

import (
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
)

// WAVE stores samples little-endian. Integer PCM is unsigned at 8 bits and
// signed above it, and IEEE float has its own format tag, so the pair of
// wFormatTag and wBitsPerSample is what names a coding.
var waveCodings = []struct {
	formatTag uint16
	coding    sample.Coding
}{
	{formatPCM, sample.U8},
	{formatPCM, sample.S16},
	{formatPCM, sample.S24},
	{formatPCM, sample.S32},
	{formatFloat, sample.F32},
	{formatFloat, sample.F64},
}

// codingOf reports the sample coding a format header declares. A tag and width
// this reader cannot express yields an invalid coding, which the caller turns
// into an unsupported-stream diagnostic.
func codingOf(formatTag uint16, bits int) sample.Coding {
	for _, entry := range waveCodings {
		if entry.formatTag == formatTag && entry.coding.Bits() == bits {
			return entry.coding
		}
	}
	return ""
}

// formatTagOf is the inverse of codingOf.
func formatTagOf(coding sample.Coding) (uint16, bool) {
	for _, entry := range waveCodings {
		if entry.coding == coding {
			return entry.formatTag, true
		}
	}
	return 0, false
}

// CodecTag names the codec a WAVE format header declares. wFormatTag alone
// does not fix the sample representation, so the coding the header resolves to
// is what a composition binds a decoder and parser to.
func CodecTag(coding sample.Coding) format.Tag { return format.NewTag("wave", string(coding)) }

// Codings lists the sample codings WAVE headers can declare, so a composition
// can bind every one of them without restating the table.
func Codings() []sample.Coding {
	result := make([]sample.Coding, 0, len(waveCodings))
	for _, entry := range waveCodings {
		result = append(result, entry.coding)
	}
	return result
}

// conventionalLayout is what a plain fmt chunk means when it states a channel
// count and no mask. One and two channels have a settled meaning; beyond that
// the header genuinely does not name its speakers, so neither does the layout.
func conventionalLayout(channels int) sample.Layout {
	switch channels {
	case 1:
		return sample.Mono()
	case 2:
		return sample.Stereo()
	default:
		return sample.Channels(channels)
	}
}

// plainHeader reports whether a 16-byte fmt chunk states everything the
// description carries, so an extensible header is written only when it adds
// valid bits or a channel mask the plain form cannot.
func plainHeader(description sample.Description) bool {
	return description.ValidBits == description.Coding.Bits() &&
		description.Layout == conventionalLayout(description.Layout.Count())
}
