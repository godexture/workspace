package wave

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/sample"
)

// Every codec a WAVE header can declare has to survive the header it is
// written into. The mux marshals a codec and signal and the demux reads them
// back, so a mismatch between the two tables shows up as a changed stream
// rather than as bytes nobody checks.
func TestFormatHeaderRoundTripsEveryCodec(t *testing.T) {
	surround := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter,
		sample.LowFrequency, sample.BackLeft, sample.BackRight)
	for _, testCase := range []struct {
		name   string
		codec  string
		signal sample.Signal
	}{
		{"u8", "u8", sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 8}},
		{"s16", "s16", sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 16}},
		{"s16-narrow", "s16", sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 12}},
		{"s24", "s24", sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 24}},
		{"s24-surround", "s24", sample.Signal{Rate: 48_000, Layout: surround, ValidBits: 20}},
		{"s32", "s32", sample.Signal{Rate: 48_000, Layout: sample.Channels(3), ValidBits: 32}},
		{"f32", "f32", sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 32}},
		{"f64", "f64", sample.Signal{Rate: 48_000, Layout: surround, ValidBits: 64}},
		// A companded header states the width of the byte holding a sample and
		// nothing about the signal in it, so the depth stays unstated.
		{"alaw", "alaw", sample.Signal{Rate: 8_000, Layout: sample.Mono()}},
		{"ulaw", "ulaw", sample.Signal{Rate: 8_000, Layout: sample.Stereo()}},
		{"ulaw-surround", "ulaw", sample.Signal{Rate: 48_000, Layout: surround}},
	} {
		t.Run(testCase.name+"/"+testCase.signal.Layout.String(), func(t *testing.T) {
			entry, ok := codecNamed(testCase.codec)
			if !ok {
				t.Fatalf("%s is not a WAVE codec", testCase.codec)
			}
			payload, blockAlign, err := marshalFormat(entry, testCase.signal, blockGeometry{})
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := inspectFormat(context.Background(), memoryRandom(payload), 0, uint64(len(payload)))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.codec.name != testCase.codec {
				t.Fatalf("codec = %s, want %s", parsed.codec.name, testCase.codec)
			}
			if parsed.signal != testCase.signal {
				t.Fatalf("signal = %#v, want %#v", parsed.signal, testCase.signal)
			}
			if parsed.blockAlign != blockAlign {
				t.Fatalf("block align = %d, want %d", parsed.blockAlign, blockAlign)
			}
			if parsed.description.Valid() != (entry.coding != "") {
				t.Fatalf("%s described a storage representation = %v", testCase.codec, parsed.description.Valid())
			}
		})
	}
}

// A plain 16-byte header states everything a full-width conventional layout
// carries. A depth below the container width, or a layout the plain form
// cannot name, is what makes the extensible header necessary.
func TestExtensibleHeaderIsWrittenOnlyWhenItAddsSomething(t *testing.T) {
	for _, testCase := range []struct {
		codec  string
		signal sample.Signal
		size   int
	}{
		{"s16", sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 16}, 16},
		{"s24", sample.Signal{Rate: 48_000, Layout: sample.Channels(6), ValidBits: 24}, 16},
		{"ulaw", sample.Signal{Rate: 8_000, Layout: sample.Mono()}, 16},
		{"s16", sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 12}, 40},
		{"s32", sample.Signal{Rate: 48_000, Layout: sample.Positions(sample.FrontLeft, sample.SideRight), ValidBits: 32}, 40},
	} {
		entry, ok := codecNamed(testCase.codec)
		if !ok {
			t.Fatalf("%s is not a WAVE codec", testCase.codec)
		}
		payload, _, err := marshalFormat(entry, testCase.signal, blockGeometry{})
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) != testCase.size {
			t.Errorf("%s %s header = %d bytes, want %d", testCase.codec, testCase.signal.Layout, len(payload), testCase.size)
		}
		if testCase.size == 40 && binary.LittleEndian.Uint16(payload[0:2]) != formatExtensible {
			t.Errorf("%s %s did not use the extensible tag", testCase.codec, testCase.signal.Layout)
		}
	}
}

func TestFormatHeaderRejectsStreamsWAVECannotState(t *testing.T) {
	entry, _ := codecNamed("s16")
	for name, signal := range map[string]sample.Signal{
		"no layout": {Rate: 48_000, ValidBits: 16},
		"no rate":   {Layout: sample.Mono(), ValidBits: 16},
	} {
		if _, _, err := marshalFormat(entry, signal, blockGeometry{}); err == nil {
			t.Errorf("%s was written into a WAVE header", name)
		}
	}
	if _, _, err := marshalFormat(waveCodec{}, sample.Signal{Rate: 48_000, Layout: sample.Mono()}, blockGeometry{}); err == nil {
		t.Error("a header was written for a codec WAVE cannot name")
	}
	if _, ok := codecForCoding(sample.S8); ok {
		t.Error("WAVE claimed a format tag for signed 8-bit PCM")
	}
}

// newLinearMuxHeader builds the header a stream stored one scalar each writes.
// Most mux tests describe such a stream, so they name it by its description.
func newLinearMuxHeader(description sample.Description) (muxHeader, error) {
	entry, ok := codecForCoding(description.Coding)
	if !ok {
		return muxHeader{}, ErrUnsupported
	}
	return newMuxHeader(entry, description.Signal, blockGeometry{})
}

func newLinearRangeMuxHeader(description sample.Description, inspected header) (muxHeader, error) {
	entry, ok := codecForCoding(description.Coding)
	if !ok {
		return muxHeader{}, ErrUnsupported
	}
	return newRangeMuxHeader(entry, description.Signal, blockGeometry{}, false, inspected)
}

func newLinearMuxHeaderWithChunks(description sample.Description, chunks muxChunks) (muxHeader, error) {
	entry, ok := codecForCoding(description.Coding)
	if !ok {
		return muxHeader{}, ErrUnsupported
	}
	return newMuxHeaderWithChunks(entry, description.Signal, blockGeometry{}, false, chunks)
}
