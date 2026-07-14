package internal

import (
	"math"
	"testing"
)

// benchmarkBlock builds a deterministic pseudo-audio stereo block: correlated
// sines with noise so the LPC, stereo-decorrelation, and Rice paths all do
// realistic work.
func benchmarkBlock(blockSize int) [][]int64 {
	left := make([]int64, blockSize)
	right := make([]int64, blockSize)
	state := uint32(0x12345678)
	for i := range left {
		state = state*1664525 + 1013904223
		noise := int64(int32(state)) >> 24
		tone := int64(12000 * math.Sin(2*math.Pi*float64(i)/128.3))
		left[i] = clamp16(tone + noise)
		state = state*1664525 + 1013904223
		right[i] = clamp16(tone + (int64(int32(state)) >> 24))
	}
	return [][]int64{left, right}
}

func clamp16(v int64) int64 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}

func BenchmarkEncodeFLACFrameDefaultConfig(b *testing.B) {
	block := benchmarkBlock(defaultEncoderBlockSize)
	options := frameOptions{
		maxFixedOrder:             defaultEncoderMaxFixedOrder,
		maxLPCOrder:               defaultEncoderMaxLPCOrder,
		maxRicePartitionOrder:     defaultEncoderMaxRiceOrder,
		enableWastedBits:          true,
		enableStereoDecorrelation: true,
		streamableSubset:          true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encodeFLACFrameWithOptions(block, 44100, 16, uint64(i), options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeFLACFrameDefaultConfig(b *testing.B) {
	block := benchmarkBlock(defaultEncoderBlockSize)
	options := frameOptions{
		maxFixedOrder:             defaultEncoderMaxFixedOrder,
		maxLPCOrder:               defaultEncoderMaxLPCOrder,
		maxRicePartitionOrder:     defaultEncoderMaxRiceOrder,
		enableWastedBits:          true,
		enableStereoDecorrelation: true,
		streamableSubset:          true,
	}
	data, err := encodeFLACFrameWithOptions(block, 44100, 16, 0, options)
	if err != nil {
		b.Fatal(err)
	}
	info := streamInfoFor(defaultEncoderBlockSize, 44100, 2, 16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decodeFLACFrame(data, info); err != nil {
			b.Fatal(err)
		}
	}
}
