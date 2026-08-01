package mixer

import (
	"math"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

func monoFrame(rate int, pts media.Pts, values []float32) *media.Frame {
	block := audio.Block{Channels: audio.Channels{values}, Layout: media.LayoutMono1, Rate: rate, PTS: pts}
	frame, err := audio.Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		panic(err)
	}
	var result media.Frame = frame
	return &result
}

func decodeMono(t *testing.T, frame media.Frame) []float32 {
	t.Helper()
	block, err := audio.Decode(&frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(block.Channels))
	}
	return block.Channels[0]
}

func assertClose(t *testing.T, got, want []float32, tol float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range want {
		if diff := float32(math.Abs(float64(got[i] - want[i]))); diff > tol {
			t.Fatalf("sample[%d] = %g, want %g", i, got[i], want[i])
		}
	}
}

func TestMixerSumsTwoInputs(t *testing.T) {
	e, err := NewEngine(2, 1, [][]float64{{1, 1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SendInput("in0", monoFrame(48000, 0, []float32{0.1, 0.2, 0.3})); err != nil {
		t.Fatal(err)
	}
	if err := e.SendInput("in1", monoFrame(48000, 0, []float32{1, 1, 1})); err != nil {
		t.Fatal(err)
	}
	if err := e.EndInput("in0"); err != nil {
		t.Fatal(err)
	}
	if err := e.EndInput("in1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	port, frame, err := e.ReceiveOutput()
	if err != nil {
		t.Fatal(err)
	}
	if port != "out0" {
		t.Fatalf("port = %q, want out0", port)
	}
	assertClose(t, decodeMono(t, frame), []float32{1.1, 1.2, 1.3}, 1e-6)

	if _, _, err := e.ReceiveOutput(); err != engine.ErrEOF {
		t.Fatalf("ReceiveOutput() error = %v, want EOF", err)
	}
}

func TestMixerActsAsTee(t *testing.T) {
	e, err := NewEngine(1, 2, [][]float64{{1}, {1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	input := []float32{0.1, -0.2, 0.3}
	if err := e.SendInput("in0", monoFrame(48000, 0, input)); err != nil {
		t.Fatal(err)
	}
	if err := e.EndInput("in0"); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	seen := map[string][]float32{}
	for i := 0; i < 2; i++ {
		port, frame, err := e.ReceiveOutput()
		if err != nil {
			t.Fatal(err)
		}
		seen[port] = decodeMono(t, frame)
	}
	assertClose(t, seen["out0"], input, 1e-6)
	assertClose(t, seen["out1"], input, 1e-6)

	if _, _, err := e.ReceiveOutput(); err != engine.ErrEOF {
		t.Fatalf("ReceiveOutput() error = %v, want EOF", err)
	}
}

func TestMixerPadsShorterStreamWithSilence(t *testing.T) {
	e, err := NewEngine(2, 1, [][]float64{{1, 1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SendInput("in0", monoFrame(48000, 0, []float32{1, 1})); err != nil {
		t.Fatal(err)
	}
	if err := e.EndInput("in0"); err != nil {
		t.Fatal(err)
	}
	if err := e.SendInput("in1", monoFrame(48000, 0, []float32{1, 1, 1, 1})); err != nil {
		t.Fatal(err)
	}
	if err := e.EndInput("in1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	var got []float32
	for {
		_, frame, err := e.ReceiveOutput()
		if err != nil {
			break
		}
		got = append(got, decodeMono(t, frame)...)
	}
	// in0 ends after 2 samples (contributes silence after); in1 runs the
	// full 4 samples, so the tail is in1 alone.
	assertClose(t, got, []float32{2, 2, 1, 1}, 1e-6)
}

func TestMixerNormalize(t *testing.T) {
	t.Run("clamps combined gain to unity", func(t *testing.T) {
		e, err := NewEngine(2, 1, [][]float64{{1, 1}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.SendInput("in0", monoFrame(48000, 0, []float32{1})); err != nil {
			t.Fatal(err)
		}
		if err := e.SendInput("in1", monoFrame(48000, 0, []float32{1})); err != nil {
			t.Fatal(err)
		}
		if err := e.EndInput("in0"); err != nil {
			t.Fatal(err)
		}
		if err := e.EndInput("in1"); err != nil {
			t.Fatal(err)
		}
		if err := e.Flush(); err != nil {
			t.Fatal(err)
		}
		_, frame, err := e.ReceiveOutput()
		if err != nil {
			t.Fatal(err)
		}
		assertClose(t, decodeMono(t, frame), []float32{1}, 1e-6)
	})

	t.Run("leaves already-safe weights untouched", func(t *testing.T) {
		e, err := NewEngine(2, 1, [][]float64{{0.5, 0.5}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.SendInput("in0", monoFrame(48000, 0, []float32{1})); err != nil {
			t.Fatal(err)
		}
		if err := e.SendInput("in1", monoFrame(48000, 0, []float32{1})); err != nil {
			t.Fatal(err)
		}
		if err := e.EndInput("in0"); err != nil {
			t.Fatal(err)
		}
		if err := e.EndInput("in1"); err != nil {
			t.Fatal(err)
		}
		if err := e.Flush(); err != nil {
			t.Fatal(err)
		}
		_, frame, err := e.ReceiveOutput()
		if err != nil {
			t.Fatal(err)
		}
		assertClose(t, decodeMono(t, frame), []float32{1}, 1e-6)
	})
}

func TestMixerRejectsChannelMismatch(t *testing.T) {
	e, err := NewEngine(2, 1, [][]float64{{1, 1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SendInput("in0", monoFrame(48000, 0, []float32{1})); err != nil {
		t.Fatal(err)
	}
	stereo := audio.Block{Channels: audio.Channels{{1}, {1}}, Layout: media.LayoutStereo2_0, Rate: 48000}
	stereoFrame, err := audio.Encode(stereo, media.SampleFormatF32P, 32)
	if err != nil {
		t.Fatal(err)
	}
	var wrapped media.Frame = stereoFrame
	if err := e.SendInput("in1", &wrapped); err == nil {
		t.Fatal("want error for channel count mismatch")
	}
}

func TestMixerDefaultWeights(t *testing.T) {
	t.Run("m=1 defaults to summing every input", func(t *testing.T) {
		if _, err := NewEngine(3, 1, nil, false); err != nil {
			t.Fatalf("NewEngine() error = %v", err)
		}
	})
	t.Run("n=1 defaults to a tee", func(t *testing.T) {
		if _, err := NewEngine(1, 3, nil, false); err != nil {
			t.Fatalf("NewEngine() error = %v", err)
		}
	})
	t.Run("n>1 and m>1 requires explicit weights", func(t *testing.T) {
		if _, err := NewEngine(2, 2, nil, false); err == nil {
			t.Fatal("want error for ambiguous default weights")
		}
	})
}

func TestNewEngineValidatesShape(t *testing.T) {
	if _, err := NewEngine(0, 1, nil, false); err == nil {
		t.Fatal("want error for zero inputs")
	}
	if _, err := NewEngine(1, 0, nil, false); err == nil {
		t.Fatal("want error for zero outputs")
	}
	if _, err := NewEngine(2, 1, [][]float64{{1, 1}, {1, 1}}, false); err == nil {
		t.Fatal("want error for wrong number of weight rows")
	}
	if _, err := NewEngine(2, 1, [][]float64{{1}}, false); err == nil {
		t.Fatal("want error for wrong row length")
	}
	if _, err := NewEngine(2, 1, [][]float64{{1, math.NaN()}}, false); err == nil {
		t.Fatal("want error for non-finite weight")
	}
}
