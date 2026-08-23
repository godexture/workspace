package wave

import (
	"strings"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
)

// waveCodec is one wFormatTag and container width a WAVE header can declare.
// The pair is what names a codec: wFormatTag alone does not fix a
// representation, and the same tag covers several widths.
type waveCodec struct {
	formatTag uint16
	bits      int
	name      string
	// coding is empty when the samples are not stored one scalar each. Such a
	// stream states a signal and leaves its representation to its codec.
	coding sample.Coding
}

// WAVE stores samples little-endian. Integer PCM is unsigned at 8 bits and
// signed above it, IEEE float has its own tag, and the companded codecs carry
// a signal wider than the byte that holds it.
var waveCodecs = []waveCodec{
	{formatPCM, 8, "u8", sample.U8},
	{formatPCM, 16, "s16", sample.S16},
	{formatPCM, 24, "s24", sample.S24},
	{formatPCM, 32, "s32", sample.S32},
	{formatFloat, 32, "f32", sample.F32},
	{formatFloat, 64, "f64", sample.F64},
	{formatALaw, 8, "alaw", ""},
	{formatULaw, 8, "ulaw", ""},
}

// codecOf reports the codec a format header declares. A tag and width this
// reader cannot name yields false, and the stream is unsupported.
func codecOf(formatTag uint16, bits int) (waveCodec, bool) {
	for _, entry := range waveCodecs {
		if entry.formatTag == formatTag && entry.bits == bits {
			return entry, true
		}
	}
	return waveCodec{}, false
}

// codecNamed is the inverse of codecOf, used when writing a header for a
// description or for a codec tag an input already carried.
func codecNamed(name string) (waveCodec, bool) {
	for _, entry := range waveCodecs {
		if entry.name == name {
			return entry, true
		}
	}
	return waveCodec{}, false
}

func codecForCoding(coding sample.Coding) (waveCodec, bool) {
	for _, entry := range waveCodecs {
		if entry.coding != "" && entry.coding == coding {
			return entry, true
		}
	}
	return waveCodec{}, false
}

// CodecTag names the codec a WAVE format header declares. wFormatTag alone does
// not fix the sample representation, so the codec the header resolves to is
// what a composition binds a decoder and parser to.
func CodecTag(name string) format.Tag { return format.NewTag("wave", name) }

// ALawTag and ULawTag name the companded codecs a WAVE header can declare.
// Their samples are one byte wide and their signal is not, so a composition
// binds them to a codec rather than to a linear representation.
func ALawTag() format.Tag { return CodecTag("alaw") }
func ULawTag() format.Tag { return CodecTag("ulaw") }

// Codings lists the sample codings WAVE headers declare for streams stored one
// scalar each, so a composition can bind every one without restating the table.
func Codings() []sample.Coding {
	result := make([]sample.Coding, 0, len(waveCodecs))
	for _, entry := range waveCodecs {
		if entry.coding != "" {
			result = append(result, entry.coding)
		}
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

// plainHeader reports whether a 16-byte fmt chunk states everything the stream
// carries, so an extensible header is written only when it adds a depth or a
// channel mask the plain form cannot.
func plainHeader(codec waveCodec, signal sample.Signal) bool {
	stated := signal.ValidBits == 0 || signal.ValidBits == codec.bits
	return stated && signal.Layout == conventionalLayout(signal.Layout.Count())
}

// codecOfTag recovers the codec a stream was tagged with, so a companded
// stream is written back under the header it arrived with.
func codecOfTag(tag format.Tag) (waveCodec, bool) {
	name, found := strings.CutPrefix(tag.String(), "wave:")
	if !found {
		return waveCodec{}, false
	}
	return codecNamed(name)
}
