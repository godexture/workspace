package audio

import (
	"testing"

	"github.com/godexture/core/domain/media"
)

func benchFrame() media.Frame {
	block := Block{
		Channels: Channels{make([]float32, 1024), make([]float32, 1024)},
		Layout:   media.LayoutStereo2_0,
		Rate:     48000,
	}
	frame, err := Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		panic(err)
	}
	return frame
}

// BenchmarkDecodeEncodeFresh is the baseline: a new Scratch every call, so
// Decode/Encode never reuse a backing array across iterations.
func BenchmarkDecodeEncodeFresh(b *testing.B) {
	frame := benchFrame()
	defer frame.Release()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		block, err := Decode(&frame)
		if err != nil {
			b.Fatal(err)
		}
		out, err := Encode(block, media.SampleFormatF32P, 32)
		if err != nil {
			b.Fatal(err)
		}
		out.Release()
	}
}

// BenchmarkDecodeEncodeScratch reuses one Scratch across every iteration,
// the way a filter engine's per-instance scratch field does.
func BenchmarkDecodeEncodeScratch(b *testing.B) {
	frame := benchFrame()
	defer frame.Release()
	var scratch Scratch
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		block, err := DecodeInto(&frame, &scratch)
		if err != nil {
			b.Fatal(err)
		}
		out, err := EncodeInto(block, media.SampleFormatF32P, 32, &scratch)
		if err != nil {
			b.Fatal(err)
		}
		out.Release()
	}
}
