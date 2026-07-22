package filter

import (
	"math"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
	"github.com/godexture/filter-audio/internal/speed"
	"github.com/godexture/filter-audio/internal/trim"
	"github.com/godexture/sdk/engine"
)

func TestResampleLinearInterpolation(t *testing.T) {
	item, err := resample.New(config.ResampleConfig{SampleRate: 4})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(2, 0, []float32{0, 1}))
	assertSamples(t, receive(t, item), []float32{0, .5})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertSamples(t, receive(t, item), []float32{1, 1})
	assertEOF(t, item)
}

func TestSpeedDoublesRateShortensOutput(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 2, Mode: config.SpeedModeInterpolate})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	assertSamples(t, output, []float32{0, 4})
	if output.(*media.AudioFrame).SampleRate != 4 {
		t.Fatalf("SampleRate = %d, want 4 (unchanged)", output.(*media.AudioFrame).SampleRate)
	}
	assertEOF(t, item)
}

func TestSpeedHalvesRateLengthensOutput(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 0.5, Mode: config.SpeedModeInterpolate})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	assertSamples(t, receive(t, item), []float32{0, 1, 2, 3, 4, 5})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertSamples(t, receive(t, item), []float32{6, 6})
	assertEOF(t, item)
}

func TestSpeedFactorOnePassesThrough(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 1, Mode: config.SpeedModeInterpolate})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if output.(*media.AudioFrame).SampleRate != 4 {
		t.Fatalf("SampleRate = %d, want 4 (unchanged)", output.(*media.AudioFrame).SampleRate)
	}
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestSpeedRelabelDoublesRateLossless(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 2, Mode: config.SpeedModeRelabel})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if output.(*media.AudioFrame).SampleRate != 8 {
		t.Fatalf("SampleRate = %d, want 8", output.(*media.AudioFrame).SampleRate)
	}
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestSpeedRelabelHalvesRateLossless(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 0.5, Mode: config.SpeedModeRelabel})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if output.(*media.AudioFrame).SampleRate != 2 {
		t.Fatalf("SampleRate = %d, want 2", output.(*media.AudioFrame).SampleRate)
	}
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestRemixDownmixesToMono(t *testing.T) {
	input := audio.Block{Channels: [][]float32{{1, -1}, {1, -1}}, Layout: media.LayoutStereo2_0, Rate: 48000}
	output, err := remix.Mix(input, config.RemixConfig{Layout: media.LayoutMono1, CenterMixDB: -3, SurroundMixDB: -3, LFEMixDB: math.Inf(-1)})
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, output.Channels[0][0], 2)
	assertFloat(t, output.Channels[0][1], -2)
}

func TestNormalizeBuffersThenScalesPeak(t *testing.T) {
	item, err := normalize.New(config.NormalizeConfig{TargetPeakDBFS: -6, AllowAmplification: true, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.5, -.25}))
	if _, err := item.ReceiveFrame(); err != engine.ErrEAGAIN {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, block.Channels[0][0], float32(math.Pow(10, -6.0/20)))
	output.Release()
	assertEOF(t, item)
}

func TestFadeAppliesBothEnds(t *testing.T) {
	item, err := fade.New(config.FadeConfig{FadeIn: 500 * time.Millisecond, FadeOut: 500 * time.Millisecond, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 10, []float32{1, 1, 1, 1}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	assertSamples(t, output, []float32{0, .5, 1, .5})
	if output.(*media.AudioFrame).Pts() != 10 {
		t.Fatalf("PTS = %d, want 10", output.(*media.AudioFrame).Pts())
	}
	assertEOF(t, item)
}

func TestTrimKeepsOnlyAudibleRange(t *testing.T) {
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 10, []float32{0, .1, 0, 0}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	assertSamples(t, output, []float32{.1})
	if output.(*media.AudioFrame).Pts() != 11 {
		t.Fatalf("PTS = %d, want 11", output.(*media.AudioFrame).Pts())
	}
	assertEOF(t, item)
}

func TestFormatLossAccountsForIntegerPrecisionReduction(t *testing.T) {
	t.Parallel()
	if got := formatLoss(media.SampleFormatS32, media.SampleFormatS16, 32, 16); got != 1 {
		t.Fatalf("S32 to S16 quality loss = %d, want 1", got)
	}
	if got := formatLoss(media.SampleFormatS16, media.SampleFormatS32, 16, 32); got != 0 {
		t.Fatalf("S16 to S32 quality loss = %d, want 0", got)
	}
}

func frame(rate int, pts media.Pts, values []float32) media.Frame {
	block := audio.Block{Channels: [][]float32{values}, Layout: media.LayoutMono1, Rate: rate, PTS: pts}
	result, err := audio.Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		panic(err)
	}
	return result
}

func send(t *testing.T, item engine.FilterEngine, input media.Frame) {
	t.Helper()
	if err := item.SendFrame(&input); err != nil {
		t.Fatal(err)
	}
	input.Release()
}

func receive(t *testing.T, item engine.FilterEngine) media.Frame {
	t.Helper()
	output, err := item.ReceiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	return *output
}

func assertSamples(t *testing.T, output media.Frame, want []float32) {
	t.Helper()
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Channels) != 1 || len(block.Channels[0]) != len(want) {
		t.Fatalf("samples = %v, want %v", block.Channels, want)
	}
	for i := range want {
		assertFloat(t, block.Channels[0][i], want[i])
	}
	output.Release()
}

func assertFloat(t *testing.T, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("value = %g, want %g", got, want)
	}
}
func assertEOF(t *testing.T, item engine.FilterEngine) {
	t.Helper()
	if _, err := item.ReceiveFrame(); err != engine.ErrEOF {
		t.Fatalf("ReceiveFrame() error = %v, want EOF", err)
	}
}
