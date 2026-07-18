//go:build goexperiment.simd && amd64

package mp3

import (
	"testing"

	"github.com/godexture/sdk/dsp"
)

func BenchmarkSynthWindowCompare(b *testing.B) {
	workspace := make([]float32, 2112)
	for i := range workspace {
		workspace[i] = float32((i*7919)%65536-32768) / 32768
	}
	window := synthesizeWindowTable[:16]
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			_, _ = synthWindowScalar(workspace, 15*64, 14, window)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for b.Loop() {
			_, _ = synthWindowSIMD(workspace, 15*64, 14, window)
		}
	})
}

func BenchmarkSynthesizeGranuleSIMDCompare(b *testing.B) {
	hasAVX2 := dsp.HasAVX2
	b.Cleanup(func() {
		dsp.HasAVX2 = hasAVX2
	})
	run := func(b *testing.B, avx2 bool) {
		dsp.HasAVX2 = avx2
		var granule [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
		var pcm [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
		var decoder Decoder
		decoder.Init()
		b.ReportAllocs()
		for b.Loop() {
			decoder.synthesizeGranule(granule[:], SamplesPerSubBandLayer3, 2, pcm[:])
		}
	}
	b.Run("scalar", func(b *testing.B) {
		run(b, false)
	})
	b.Run("simd", func(b *testing.B) {
		run(b, hasAVX2)
	})
}
