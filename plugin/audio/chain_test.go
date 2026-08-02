package filter

import (
	"math"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/gain"
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
// filter nodes, without depending on pipeline wiring itself. It takes
// ownership of input and releases it (and every intermediate stage output)
// once consumed by the next stage's SendFrame -- gain.Engine.SendFrame
// only reads its input (via DecodeInto) rather than retaining it, so
// nothing else may still reference input by the time this returns. Only
// the final output is left unreleased, for the caller to consume/release.
// A caller that wants to keep its own reference to input alive afterward
// must Retain it before calling.
func runChain(t testing.TB, stages []engine.FilterEngine, input media.Frame) media.Frame {
	t.Helper()
	current := input
	for i, stage := range stages {
		if err := stage.SendFrame(&current); err != nil {
			t.Fatalf("stage %d SendFrame() error = %v", i, err)
		}
		current.Release()
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

// BenchmarkGainChainDepths is chain_pipeline_test.go's single-goroutine,
// direct-call reference: SendFrame/ReceiveFrame through the gain stages
// sequentially in the calling goroutine, with none of core/pipeline's
// scheduler/edge/ownership machinery -- not a strict "lower bound", since
// BenchmarkGainChainPipeline's per-node goroutines can overlap Encode/
// gain/Decode across in-flight frames on separate cores in a way this
// benchmark structurally cannot (see that benchmark's doc comment). Depth
// and block size sub-benchmarks match BenchmarkGainChainPipeline's exactly
// (same chainDepths x chainBlockSizes, same chainFrameCount frames per
// op), and each frame is freshly Encoded before the chain and Decoded
// after, the same per-frame cost BenchmarkGainChainPipeline's source/sink
// nodes pay -- so this benchmark's plain ns/op is directly comparable to
// that one's processing-ns/op.
//
// Stage construction is excluded from the timed region (built once per
// sub-benchmark, matching Prepare's exclusion on the pipeline side) and
// closed once at the end via b.Cleanup; gain.Engine doesn't expose Close
// through the engine.FilterEngine interface, so stageCloser asserts for it
// where present.
func BenchmarkGainChainDepths(b *testing.B) {
	for _, depth := range chainDepths {
		for _, size := range chainBlockSizes {
			b.Run(depthName(depth)+"/"+size.name, func(b *testing.B) {
				block := stereoBlock(size.frames)
				stages := newGainChain(b, depth)
				b.Cleanup(func() {
					for _, stage := range stages {
						if closer, ok := stage.(stageCloser); ok {
							_ = closer.Close()
						}
					}
				})
				b.ReportAllocs()
				b.SetBytes(int64(size.frames * 2 * 4 * chainFrameCount))
				for i := 0; i < b.N; i++ {
					for f := 0; f < chainFrameCount; f++ {
						encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
						if err != nil {
							b.Fatal(err)
						}
						out := runChain(b, stages, encoded)
						if _, err := audio.Decode(&out); err != nil {
							b.Fatal(err)
						}
						out.Release()
					}
				}
			})
		}
	}
}

// stageCloser is asserted against each engine.FilterEngine stage to close
// it after a benchmark reuses it across many iterations; gain.Engine
// implements Close but engine.FilterEngine itself doesn't require it.
type stageCloser interface {
	Close() error
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
