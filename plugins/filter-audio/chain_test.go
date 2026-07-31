package filter

import (
	"math"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/plugins/filter-audio/internal/gain"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

// chainDepths is the M0 baseline shape from docs/refactor/quality.md's
// "1/4/16段の軽量 audio filter chain" requirement.
var chainDepths = []int{1, 4, 16}

const chainStageDecibels = -1.0

func newGainChain(t testing.TB, depth int) []engine.FilterEngine {
	t.Helper()
	stages := make([]engine.FilterEngine, depth)
	for i := range stages {
		item, err := gain.New(config.GainConfig{Decibels: chainStageDecibels})
		if err != nil {
			t.Fatalf("gain.New() error = %v", err)
		}
		stages[i] = item
	}
	return stages
}

// runChain pushes input through each stage's SendFrame/ReceiveFrame in
// sequence, mirroring how core/pipeline links single-input single-output
// filter nodes, without depending on pipeline wiring itself.
func runChain(t testing.TB, stages []engine.FilterEngine, input media.Frame) media.Frame {
	t.Helper()
	current := input
	for i, stage := range stages {
		if err := stage.SendFrame(&current); err != nil {
			t.Fatalf("stage %d SendFrame() error = %v", i, err)
		}
		out, err := stage.ReceiveFrame()
		if err != nil {
			t.Fatalf("stage %d ReceiveFrame() error = %v", i, err)
		}
		current = out
	}
	return current
}

func TestGainChainDepthsProduceExpectedAttenuation(t *testing.T) {
	t.Parallel()
	input := []float32{0.5, -0.25, 0.125, -0.0625, 1, -1, 0.0001, -0.9999}
	stageFactor := float32(math.Pow(10, chainStageDecibels/20))

	for _, depth := range chainDepths {
		stages := newGainChain(t, depth)
		output := runChain(t, stages, frame(48000, 0, append([]float32(nil), input...)))

		want := make([]float32, len(input))
		for i, v := range input {
			want[i] = v * float32(math.Pow(float64(stageFactor), float64(depth)))
		}
		assertSamplesTol(t, output, want, 1e-4)
	}
}

func BenchmarkGainChainDepths(b *testing.B) {
	for _, depth := range chainDepths {
		b.Run(depthName(depth), func(b *testing.B) {
			block := stereoBlock(4096)
			b.ReportAllocs()
			b.SetBytes(int64(4096 * 2 * 4))
			for i := 0; i < b.N; i++ {
				stages := newGainChain(b, depth)
				encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
				if err != nil {
					b.Fatal(err)
				}
				out := runChain(b, stages, encoded)
				out.Release()
			}
		})
	}
}

func depthName(depth int) string {
	switch depth {
	case 1:
		return "1stage"
	case 4:
		return "4stage"
	case 16:
		return "16stage"
	default:
		return "Nstage"
	}
}
