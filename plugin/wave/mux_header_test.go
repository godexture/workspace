package wave

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/godexture/godec/media/sample"
)

func TestMuxHeaderProducesFixedLengthRIFFAndExtensiblePCM(t *testing.T) {
	tests := []struct {
		name        string
		description sample.Description
		headerSize  int
	}{
		{
			name:        "pcm",
			description: sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian},
			headerSize:  80,
		},
		{
			name:        "extensible valid bits",
			description: sample.Description{Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000, Layout: sample.Mono, Endian: sample.LittleEndian},
			headerSize:  104,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, err := newMuxHeader(test.description)
			if err != nil {
				t.Fatal(err)
			}
			if len(header.initial) != test.headerSize || string(header.initial[0:4]) != tagRIFF || string(header.initial[12:16]) != "JUNK" {
				t.Fatalf("initial header = size %d, root %q, reserve %q", len(header.initial), header.initial[0:4], header.initial[12:16])
			}
			data := make([]byte, int(header.blockAlign)*2)
			encoded := materializeMuxHeader(t, header, data)
			inspected, err := inspectHeader(t.Context(), memoryRandom(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if inspected.description != test.description || inspected.dataOffset != header.dataOffset || inspected.dataSize != uint64(len(data)) || inspected.rf64 {
				t.Fatalf("round-trip header = %#v", inspected)
			}
		})
	}
}

func TestMuxHeaderSwitchesReservedChunkAtRIFFBoundary(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian}
	header, err := newMuxHeader(description)
	if err != nil {
		t.Fatal(err)
	}
	maximumRIFFData := (uint64(math.MaxUint32) - uint64(header.dataOffset-8)) &^ (header.blockAlign - 1)
	riff, err := header.finalize(maximumRIFFData)
	if err != nil {
		t.Fatal(err)
	}
	if riff.rf64 || riff.fileSize-8 > math.MaxUint32 || len(riff.patches) != 2 {
		t.Fatalf("RIFF boundary = %#v", riff)
	}
	rf64, err := header.finalize(maximumRIFFData + header.blockAlign)
	if err != nil {
		t.Fatal(err)
	}
	if !rf64.rf64 || len(rf64.patches) != 3 || string(rf64.patches[0].payload[0:4]) != tagRF64 || string(rf64.patches[1].payload[0:4]) != tagDS64 {
		t.Fatalf("RF64 boundary = %#v", rf64)
	}
	if got := binary.LittleEndian.Uint64(rf64.patches[1].payload[16:24]); got != maximumRIFFData+header.blockAlign {
		t.Fatalf("ds64 data size = %d", got)
	}
	if len(header.initial) != 80 {
		t.Fatalf("RF64 planning changed initial header length to %d", len(header.initial))
	}
}

func TestMuxHeaderAccountsOddDataPadding(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	header, err := newMuxHeader(description)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := header.finalize(3)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.padding != 1 || finalized.fileSize != uint64(header.dataOffset)+4 {
		t.Fatalf("odd payload finalization = %#v", finalized)
	}
}

func materializeMuxHeader(t *testing.T, header muxHeader, data []byte) []byte {
	t.Helper()
	finalized, err := header.finalize(uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	value := make([]byte, int(finalized.fileSize))
	copy(value, header.initial)
	copy(value[int(header.dataOffset):], data)
	for _, patch := range finalized.patches {
		copy(value[int(patch.offset):], patch.payload)
	}
	return value
}
