package layer12

import (
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/sdk/bits"
)

func TestDecodeZeroAllocationBlocks(t *testing.T) {
	for _, test := range []struct {
		name       string
		header     frame.Header
		blockCount int
		channels   int
	}{
		{name: "layer1 mono", header: testHeader(t, 3, 3, 1, 0, frame.Mono, 0), blockCount: 1, channels: 1},
		{name: "layer1 stereo", header: testHeader(t, 3, 3, 1, 0, frame.Stereo, 0), blockCount: 1, channels: 2},
		{name: "layer2 mono", header: testHeader(t, 3, 2, 8, 0, frame.Mono, 0), blockCount: 3, channels: 1},
		{name: "layer2 stereo", header: testHeader(t, 3, 2, 8, 0, frame.Stereo, 0), blockCount: 3, channels: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := zeroAllocationPayload(test.header)
			var decoder Decoder
			reader := bits.New(payload)
			var callbacks []decodeCallback
			err := decoder.Decode(reader, test.header, func(granule []float32, block int) {
				callbacks = append(callbacks, decodeCallback{block: block, samples: slices.Clone(granule)})
			})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if reader.Overrun() {
				t.Fatal("valid zero-allocation payload overran")
			}
			if len(callbacks) != test.blockCount {
				t.Fatalf("callback count = %d, want %d", len(callbacks), test.blockCount)
			}
			for index, callback := range callbacks {
				if callback.block != index {
					t.Errorf("callback %d block = %d, want %d", index, callback.block, index)
				}
				if got, want := len(callback.samples), test.channels*granuleSamples; got != want {
					t.Errorf("callback %d granule length = %d, want %d", index, got, want)
				}
				for sampleIndex, sample := range callback.samples {
					if sample != 0 {
						t.Fatalf("callback %d sample %d = %v, want zero", index, sampleIndex, sample)
					}
				}
			}
		})
	}
}

func TestDecodeTruncationDoesNotCallback(t *testing.T) {
	header := testHeader(t, 3, 2, 8, 0, frame.Stereo, 0)
	payload := zeroAllocationPayload(header)
	payload = payload[:len(payload)-1]

	var decoder Decoder
	reader := bits.New(payload)
	callbacks := 0
	err := decoder.Decode(reader, header, func([]float32, int) { callbacks++ })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if callbacks != 0 {
		t.Fatalf("truncated payload made %d callbacks, want none", callbacks)
	}
}

func TestDecodeLayer1PreservesSampleBitOrder(t *testing.T) {
	header := testHeader(t, 3, 3, 1, 0, frame.Mono, 0)
	codes := []uint32{
		3, 0, 1, 2, 2, 1, 0, 3,
		1, 3, 2, 0, 0, 2, 3, 1,
		2, 3, 0, 1, 3, 1, 2, 0,
	}
	payload := layer1Payload(codes)

	var decoder Decoder
	reader := bits.New(payload)
	var got [granuleSamples]float32
	callbacks := 0
	if err := decoder.Decode(reader, header, func(granule []float32, block int) {
		callbacks++
		if block != 0 {
			t.Fatalf("callback block = %d, want 0", block)
		}
		copy(got[:], granule)
	}); err != nil {
		t.Fatal(err)
	}
	if callbacks != 1 {
		t.Fatalf("callback count = %d, want 1", callbacks)
	}

	scale := dequantizationTable[0] * float32(1<<21)
	codeIndex := 0
	for pass := 0; pass < 3; pass++ {
		for sample := 0; sample < 4; sample++ {
			left := float32(int(codes[codeIndex])-1) * scale
			codeIndex++
			right := float32(int(codes[codeIndex])-1) * scale
			codeIndex++
			offset := pass*4 + sample
			if got[offset] != left {
				t.Fatalf("band 0 sample %d = %v, want %v", offset, got[offset], left)
			}
			if got[granuleStride+offset] != right {
				t.Fatalf("band 1 sample %d = %v, want %v", offset, got[granuleStride+offset], right)
			}
		}
	}
}

func TestDecodeClearsBlocksBeforeReuse(t *testing.T) {
	header := testHeader(t, 3, 2, 8, 0, frame.Mono, 0)
	payload := zeroAllocationPayload(header)

	var decoder Decoder
	truncated := bits.New(payload[:len(payload)-1])
	if err := decoder.Decode(truncated, header, func([]float32, int) {}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Decode(truncated) error = %v, want io.ErrUnexpectedEOF", err)
	}
	decoder.granules[0][0] = 1

	reader := bits.New(payload)
	if err := decoder.Decode(reader, header, func(granule []float32, block int) {
		for index, sample := range granule {
			if sample != 0 {
				t.Fatalf("reused decoder block %d sample %d = %v, want zero", block, index, sample)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSubbandAllocationTables(t *testing.T) {
	for _, test := range []struct {
		name        string
		header      frame.Header
		allocation  []subbandAllocation
		totalBands  uint8
		stereoBands uint8
	}{
		{name: "layer1", header: testHeader(t, 3, 3, 1, 0, frame.Stereo, 0), allocation: allocationTable1[:], totalBands: 32, stereoBands: 32},
		{name: "mpeg2", header: testHeader(t, 2, 2, 8, 0, frame.Stereo, 0), allocation: allocationTable2MPEG2[:], totalBands: 30, stereoBands: 30},
		{name: "mpeg1 standard", header: testHeader(t, 3, 2, 8, 1, frame.Stereo, 0), allocation: allocationTable2MPEG1[:], totalBands: 27, stereoBands: 27},
		{name: "mpeg1 low rate", header: testHeader(t, 3, 2, 1, 2, frame.Mono, 0), allocation: allocationTable2MPEG1LowRate[:], totalBands: 12, stereoBands: 0},
		{name: "mpeg1 high rate", header: testHeader(t, 3, 2, 11, 0, frame.JointStereo, 2), allocation: allocationTable2MPEG1[:], totalBands: 30, stereoBands: 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			var scaleFactors scaleFactorInfo
			got := subbandAllocationTable(test.header, &scaleFactors)
			if !slices.Equal(got, test.allocation) {
				t.Fatalf("allocation table = %#v, want %#v", got, test.allocation)
			}
			if scaleFactors.totalBands != test.totalBands || scaleFactors.stereoBands != test.stereoBands {
				t.Fatalf("band selection = (%d, %d), want (%d, %d)", scaleFactors.totalBands, scaleFactors.stereoBands, test.totalBands, test.stereoBands)
			}
		})
	}
}

func TestApplyScaleFactorsUsesGranuleStride(t *testing.T) {
	var scaleFactors scaleFactorInfo
	scaleFactors.totalBands = 3
	scaleFactors.stereoBands = 1
	for band := 0; band < int(scaleFactors.totalBands); band++ {
		scaleFactors.scaleFactors[band*6] = float32(band + 2)
		scaleFactors.scaleFactors[band*6+3] = float32(band + 5)
	}

	var destination [granuleSamples * channels]float32
	for band := 0; band < int(scaleFactors.totalBands); band++ {
		for sample := 0; sample < granuleStride; sample++ {
			destination[band*granuleStride+sample] = float32(band*100 + sample + 1)
			destination[granuleSamples+band*granuleStride+sample] = float32(-band*100 - sample - 1)
		}
	}
	applyScaleFactors(&scaleFactors, 0, destination[:])

	for band := 0; band < int(scaleFactors.totalBands); band++ {
		for sample := 0; sample < granuleStride; sample++ {
			original := float32(band*100 + sample + 1)
			left := original * float32(band+2)
			rightSource := float32(-band*100 - sample - 1)
			if band >= int(scaleFactors.stereoBands) {
				rightSource = original
			}
			right := rightSource * float32(band+5)
			if got := destination[band*granuleStride+sample]; got != left {
				t.Fatalf("left band %d sample %d = %v, want %v", band, sample, got, left)
			}
			if got := destination[granuleSamples+band*granuleStride+sample]; got != right {
				t.Fatalf("right band %d sample %d = %v, want %v", band, sample, got, right)
			}
		}
	}
}

func TestDecodeAllocations(t *testing.T) {
	for _, test := range []struct {
		name   string
		header frame.Header
	}{
		{name: "layer1", header: testHeader(t, 3, 3, 1, 0, frame.Mono, 0)},
		{name: "layer2", header: testHeader(t, 3, 2, 8, 0, frame.Stereo, 0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := zeroAllocationPayload(test.header)
			var decoder Decoder
			var reader bits.Reader
			allocations := testing.AllocsPerRun(100, func() {
				reader.Init(payload, 0, int32(len(payload))*8)
				if err := decoder.Decode(&reader, test.header, discard); err != nil {
					panic(err)
				}
			})
			if allocations != 0 {
				t.Fatalf("allocations per Decode = %v, want zero", allocations)
			}
		})
	}
}

type decodeCallback struct {
	block   int
	samples []float32
}

func discard([]float32, int) {}

func zeroAllocationPayload(header frame.Header) []byte {
	var scaleFactors scaleFactorInfo
	allocation := subbandAllocationTable(header, &scaleFactors)
	allocationIndex := 0
	nextBoundary := 0
	width := 0
	bitCount := 0
	for band := 0; band < int(scaleFactors.totalBands); band++ {
		if band == nextBoundary {
			entry := allocation[allocationIndex]
			allocationIndex++
			nextBoundary += int(entry.bandCount)
			width = int(entry.codeWidth)
		}
		bitCount += width
		if band < int(scaleFactors.stereoBands) {
			bitCount += width
		}
	}
	return make([]byte, (bitCount+7)/8)
}

func layer1Payload(codes []uint32) []byte {
	const allocationBits = subbands * 4
	const scaleFactorBits = 2 * 6
	payload := make([]byte, (allocationBits+scaleFactorBits+len(codes)*2+7)/8)
	bitIndex := 0
	write := func(value uint32, width int) {
		for shift := width - 1; shift >= 0; shift-- {
			if value&(1<<shift) != 0 {
				payload[bitIndex/8] |= 1 << (7 - bitIndex%8)
			}
			bitIndex++
		}
	}
	for band := 0; band < subbands; band++ {
		value := uint32(0)
		if band < 2 {
			value = 1
		}
		write(value, 4)
	}
	write(0, 6)
	write(0, 6)
	for _, code := range codes {
		write(code, 2)
	}
	return payload
}

func testHeader(t testing.TB, versionID, layerID, bitrate, rate byte, mode frame.ChannelMode, modeExtension byte) frame.Header {
	t.Helper()
	raw := [4]byte{
		0xff,
		0xe0 | versionID<<3 | layerID<<1 | 1,
		bitrate<<4 | rate<<2,
		byte(mode)<<6 | modeExtension<<4,
	}
	header, err := frame.Parse(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	return header
}
