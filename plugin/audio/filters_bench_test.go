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

// stereoBlockSeed1/2 seed stereoBlock's generator. Fixed rather than
// runtime-seeded (math/rand/v2's package-level functions seed themselves
// from the OS at process start) so the same call produces byte-identical
// input on every run and every machine -- required for
// docs/refactor/baseline.manifest.json to name a reproducible input rather
// than "whatever the process happened to seed itself with".
const (
	stereoBlockSeed1 = 0x9e3779b97f4a7c15
	stereoBlockSeed2 = 0xbf58476d1ce4e5b9
)

func stereoBlock(samples int) audio.Block {
	gen := rand.New(rand.NewPCG(stereoBlockSeed1, stereoBlockSeed2))
	block := audio.Block{Channels: make([][]float32, 2), Layout: media.LayoutStereo2_0, Rate: 48000}
	for channel := range block.Channels {
		block.Channels[channel] = make([]float32, samples)
		for i := range block.Channels[channel] {
			block.Channels[channel][i] = gen.Float32()*2 - 1
		}
	}
	return block
}
