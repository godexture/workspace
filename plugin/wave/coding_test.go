package wave

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/sample"
)

// Every coding a WAVE header can declare has to survive the header it is
// written into. The mux marshals a description and the demux reads it back, so
// a mismatch between the two tables shows up as a changed description rather
// than as bytes nobody checks.
func TestFormatHeaderRoundTripsEveryCoding(t *testing.T) {
	surround := sample.Positions(sample.FrontLeft, sample.FrontRight, sample.FrontCenter,
		sample.LowFrequency, sample.BackLeft, sample.BackRight)
	for _, description := range []sample.Description{
		waveDescription(sample.U8, sample.Mono(), 8),
		waveDescription(sample.S16, sample.Stereo(), 16),
		waveDescription(sample.S16, sample.Stereo(), 12),
		waveDescription(sample.S24, sample.Mono(), 24),
		waveDescription(sample.S24, surround, 20),
		waveDescription(sample.S32, sample.Channels(3), 32),
		waveDescription(sample.F32, sample.Stereo(), 32),
		waveDescription(sample.F64, surround, 64),
	} {
		t.Run(string(description.Coding)+"/"+description.Layout.String(), func(t *testing.T) {
			payload, blockAlign, err := marshalFormat(description)
			if err != nil {
				t.Fatal(err)
			}
			if blockAlign != description.BlockBytes() {
				t.Fatalf("block align = %d, want %d", blockAlign, description.BlockBytes())
			}
			got, gotAlign, err := inspectFormat(context.Background(), memoryRandom(payload), 0, uint64(len(payload)))
			if err != nil {
				t.Fatal(err)
			}
			if got != description || gotAlign != blockAlign {
				t.Fatalf("round trip = %#v (align %d), want %#v", got, gotAlign, description)
			}
		})
	}
}

// A plain 16-byte header states everything a full-width conventional layout
// carries. Valid bits below the container width, or a layout the plain form
// cannot name, are what make the extensible header necessary.
func TestExtensibleHeaderIsWrittenOnlyWhenItAddsSomething(t *testing.T) {
	for _, testCase := range []struct {
		description sample.Description
		size        int
	}{
		{waveDescription(sample.S16, sample.Stereo(), 16), 16},
		{waveDescription(sample.S24, sample.Channels(6), 24), 16},
		{waveDescription(sample.S16, sample.Stereo(), 12), 40},
		{waveDescription(sample.S32, sample.Positions(sample.FrontLeft, sample.SideRight), 32), 40},
	} {
		payload, _, err := marshalFormat(testCase.description)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) != testCase.size {
			t.Errorf("%s %s header = %d bytes, want %d", testCase.description.Coding, testCase.description.Layout, len(payload), testCase.size)
		}
		if testCase.size == 40 && binary.LittleEndian.Uint16(payload[0:2]) != formatExtensible {
			t.Errorf("%s %s did not use the extensible tag", testCase.description.Coding, testCase.description.Layout)
		}
	}
}

func TestFormatHeaderRejectsRepresentationsWAVECannotState(t *testing.T) {
	for name, description := range map[string]sample.Description{
		"signed 8 bit":  waveDescription(sample.S8, sample.Mono(), 8),
		"big endian":    {Signal: sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 16}, Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.BigEndian},
		"planar":        waveDescription(sample.S16, sample.Mono(), 16).Decoded(),
		"no layout":     {Signal: sample.Signal{Rate: 48_000, ValidBits: 16}, Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.LittleEndian},
		"unknown rate":  {Signal: sample.Signal{Layout: sample.Mono(), ValidBits: 16}, Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.LittleEndian},
		"unknown codec": {Signal: sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 20}, Coding: "s20", Packing: sample.Interleaved, Endian: sample.LittleEndian},
	} {
		if _, _, err := marshalFormat(description); err == nil {
			t.Errorf("%s was written into a WAVE header", name)
		}
	}
}

func waveDescription(coding sample.Coding, layout sample.Layout, validBits int) sample.Description {
	endian := sample.LittleEndian
	if coding.Bytes() == 1 {
		endian = sample.NoEndian
	}
	return sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: layout, ValidBits: validBits},
		Coding:  coding,
		Packing: sample.Interleaved,
		Endian:  endian,
	}
}
