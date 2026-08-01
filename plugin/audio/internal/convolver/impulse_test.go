package convolver

import (
	"math"
	"reflect"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/dsp/fft"
)

// TestBuildPartitionsWorkerCountDoesNotChangeOutput is the convolver's
// baseline for the M0 "worker 1/4/16 semantic diff" contract (see
// docs/refactor/performance.md): buildPartitions is the only place the
// engine hands work to a registry.WorkerPool, so pool size must not change
// the forward-transformed partitions it produces.
func TestBuildPartitionsWorkerCountDoesNotChangeOutput(t *testing.T) {
	const hop = 64
	plan, err := fft.NewRealPlan(2 * hop)
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]float32, hop*10)
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i) * 0.31))
	}

	want, err := buildPartitions(plan, hop, samples, nil)
	if err != nil {
		t.Fatalf("sequential buildPartitions() error = %v", err)
	}

	for _, workers := range []int{1, 4, 16} {
		pool := registry.NewWorkerPool(workers)
		t.Cleanup(func() { pool.Close() })

		got, err := buildPartitions(plan, hop, samples, pool)
		if err != nil {
			t.Fatalf("workers=%d: buildPartitions() error = %v", workers, err)
		}
		if len(got) != len(want) {
			t.Fatalf("workers=%d: partition count = %d, want %d", workers, len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(got[i].spectrum, want[i].spectrum) {
				t.Fatalf("workers=%d: partition %d spectrum differs from the sequential build", workers, i)
			}
		}
	}
}

// TestEngineWorkerCountDoesNotChangeEndToEndOutput is
// docs/refactor/checkpoint.md M0-R1's follow-up to the buildPartitions-only
// test above: buildPartitions() output feeding an unrelated internal
// struct comparison doesn't prove the *filter's actual output samples* are
// worker-count invariant. Drive the real Engine (Prepare/SendFrame/
// ReceiveFrame, the same path production code uses) with impulse-building
// pool sizes 1/4/16 and compare the full processed sample stream.
func TestEngineWorkerCountDoesNotChangeEndToEndOutput(t *testing.T) {
	const hop = 64
	ir := make([]float32, hop*10)
	for i := range ir {
		ir[i] = float32(math.Sin(float64(i)*0.19)) * 0.1
	}
	input := make([]float32, hop*20)
	for i := range input {
		input[i] = float32(math.Cos(float64(i) * 0.07))
	}

	want := runConvolverEndToEnd(t, ir, input, nil)

	for _, workers := range []int{1, 4, 16} {
		pool := registry.NewWorkerPool(workers)
		t.Cleanup(func() { pool.Close() })

		got := runConvolverEndToEnd(t, ir, input, pool)
		if len(got) != len(want) {
			t.Fatalf("workers=%d: output length = %d, want %d", workers, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("workers=%d: sample %d = %v, want %v (end-to-end output differs from the sequential build)", workers, i, got[i], want[i])
			}
		}
	}
}

// runConvolverEndToEnd builds a fresh Engine, prepares it against pool (nil
// for sequential impulse construction), pushes input through SendFrame/
// ReceiveFrame in one block plus a Flush, and returns the concatenated
// output samples.
func runConvolverEndToEnd(t *testing.T, impulse, input []float32, pool *registry.WorkerPool) []float32 {
	t.Helper()
	engine, err := New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{impulse},
		WetDryMix:       1,
		BlockSize:       64,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := engine.Prepare(registry.ResourceGrant{Pool: pool}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	block := audio.Block{Channels: [][]float32{input}, Layout: media.LayoutMono1, Rate: 48000}
	encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		t.Fatalf("audio.Encode() error = %v", err)
	}
	var frame media.Frame = encoded
	if err := engine.SendFrame(&frame); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}
	if err := engine.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	var out []float32
	for {
		frame, err := engine.ReceiveFrame()
		if err != nil {
			break
		}
		decoded, err := audio.Decode(&frame)
		if err != nil {
			t.Fatalf("audio.Decode() error = %v", err)
		}
		out = append(out, decoded.Channels[0]...)
		frame.Release()
	}
	return out
}
