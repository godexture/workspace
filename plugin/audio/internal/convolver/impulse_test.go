package convolver

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/dsp/fft"
	"github.com/godexture/godec/sdk/engine"
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
// pool sizes 1/4/16 and compare frame count, each frame's sample count and
// PTS, and the sample data itself -- not just a flat concatenated sample
// stream, which would miss a regression that reshuffles frame boundaries
// or timestamps while keeping the sample sequence intact.
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
	if len(want) == 0 {
		t.Fatal("sequential build produced no output frames")
	}

	for _, workers := range []int{1, 4, 16} {
		pool := registry.NewWorkerPool(workers)
		t.Cleanup(func() { pool.Close() })

		got := runConvolverEndToEnd(t, ir, input, pool)
		if len(got) != len(want) {
			t.Fatalf("workers=%d: output frame count = %d, want %d", workers, len(got), len(want))
		}
		for f := range want {
			if got[f].pts != want[f].pts {
				t.Fatalf("workers=%d: frame %d PTS = %v, want %v", workers, f, got[f].pts, want[f].pts)
			}
			if len(got[f].samples) != len(want[f].samples) {
				t.Fatalf("workers=%d: frame %d sample count = %d, want %d", workers, f, len(got[f].samples), len(want[f].samples))
			}
			for i := range want[f].samples {
				if got[f].samples[i] != want[f].samples[i] {
					t.Fatalf("workers=%d: frame %d sample %d = %v, want %v (end-to-end output differs from the sequential build)", workers, f, i, got[f].samples[i], want[f].samples[i])
				}
			}
		}
	}
}

// receivedFrame is one decoded output frame's observable contract: its
// timestamp and sample data. Concatenating every frame's samples into one
// flat slice (the previous shape of this test) would hide a regression
// that reshuffles frame boundaries or PTS while the overall sample
// sequence stays byte-identical.
type receivedFrame struct {
	pts     media.Pts
	samples []float32
}

// runConvolverEndToEnd builds a fresh Engine, prepares it against pool (nil
// for sequential impulse construction), pushes input through SendFrame/
// ReceiveFrame in one block plus a Flush, and returns each decoded output
// frame's PTS and sample data in delivery order. It releases the input
// frame after SendFrame (the SendFrame contract used throughout this
// package: see plugin/audio/filters_test.go's send helper) and always
// closes the Engine, so a failure partway through still exercises the
// Engine's queued-frame cleanup on Close instead of leaking it silently.
func runConvolverEndToEnd(t *testing.T, impulse, input []float32, pool *registry.WorkerPool) []receivedFrame {
	t.Helper()
	eng, err := New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{impulse},
		WetDryMix:       1,
		BlockSize:       64,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := eng.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := eng.Prepare(registry.ResourceGrant{Pool: pool}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	block := audio.Block{Channels: [][]float32{input}, Layout: media.LayoutMono1, Rate: 48000}
	encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		t.Fatalf("audio.Encode() error = %v", err)
	}
	var frame media.Frame = encoded
	sendErr := eng.SendFrame(&frame)
	frame.Release()
	if sendErr != nil {
		t.Fatalf("SendFrame() error = %v", sendErr)
	}
	if err := eng.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	var out []receivedFrame
	for {
		frame, err := eng.ReceiveFrame()
		if errors.Is(err, engine.ErrEOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReceiveFrame() error = %v", err)
		}
		decoded, err := audio.Decode(&frame)
		if err != nil {
			frame.Release()
			t.Fatalf("audio.Decode() error = %v", err)
		}
		out = append(out, receivedFrame{
			pts:     frame.Pts(),
			samples: append([]float32(nil), decoded.Channels[0]...),
		})
		frame.Release()
	}
	return out
}
