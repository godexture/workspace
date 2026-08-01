package filter

import (
	"math/rand/v2"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/gain"
	"github.com/godexture/godec/plugin/audio/internal/remix"
	"github.com/godexture/godec/sdk/audio"
)

func BenchmarkGainStereoF32(b *testing.B) {
	values := stereoBlock(4096)
	b.ReportAllocs()
	b.SetBytes(int64(4096 * 2 * 4))
	for i := 0; i < b.N; i++ {
		block := values.Clone()
		for _, channel := range block.Channels {
			gain.Apply(channel, 0.75)
		}
	}
}

func BenchmarkRemixStereoToMonoF32(b *testing.B) {
	values := stereoBlock(4096)
	config := config.RemixConfig{Layout: media.LayoutMono1, CenterMixDB: -3.010299956639812, SurroundMixDB: -3.010299956639812, LFEMixDB: -1000, Normalize: true}
	b.ReportAllocs()
	b.SetBytes(int64(4096 * 2 * 4))
	for i := 0; i < b.N; i++ {
		if _, err := remix.Mix(values, config); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRemixStereoToMonoF32Scratch is BenchmarkRemixStereoToMonoF32's
// counterpart using MixInto with a scratch buffer reused across iterations,
// the way remix.Engine.SendFrame does.
func BenchmarkRemixStereoToMonoF32Scratch(b *testing.B) {
	values := stereoBlock(4096)
	config := config.RemixConfig{Layout: media.LayoutMono1, CenterMixDB: -3.010299956639812, SurroundMixDB: -3.010299956639812, LFEMixDB: -1000, Normalize: true}
	var scratch audio.Channels
	b.ReportAllocs()
	b.SetBytes(int64(4096 * 2 * 4))
	for i := 0; i < b.N; i++ {
		if _, err := remix.MixInto(values, config, &scratch); err != nil {
			b.Fatal(err)
		}
	}
}

func stereoBlock(samples int) audio.Block {
	block := audio.Block{Channels: make([][]float32, 2), Layout: media.LayoutStereo2_0, Rate: 48000}
	for channel := range block.Channels {
		block.Channels[channel] = make([]float32, samples)
		for i := range block.Channels[channel] {
			block.Channels[channel][i] = rand.Float32()*2 - 1
		}
	}
	return block
}
