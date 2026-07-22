package filter

import (
	"math"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/compressor"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/eq"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/gate"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
	"github.com/godexture/filter-audio/internal/reverb"
	"github.com/godexture/filter-audio/internal/speed"
	"github.com/godexture/filter-audio/internal/trim"
	"github.com/godexture/sdk/audio"
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

func TestSpeedRelabelPTSAdvancesBySampleCount(t *testing.T) {
	item, err := speed.New(config.SpeedConfig{Factor: 100, Mode: config.SpeedModeRelabel})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	first := receive(t, item)
	if got := first.(*media.AudioFrame).Pts(); got != 0 {
		t.Fatalf("first PTS = %d, want 0", got)
	}
	first.Release()

	send(t, item, frame(4, 4, []float32{8, 10, 12, 14}))
	second := receive(t, item)
	if got := second.(*media.AudioFrame).Pts(); got != 4 {
		t.Fatalf("second PTS = %d, want 4 (advances by the block's own sample count, not scaled by Factor)", got)
	}
	second.Release()

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
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, TrimMode: config.TrimModeBoth, MemoryLimitBytes: 1})
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

func TestTrimModeStartKeepsTrailingSilence(t *testing.T) {
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, TrimMode: config.TrimModeStart, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 10, []float32{0, .1, 0, 0}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	assertSamples(t, output, []float32{.1, 0, 0})
	if output.(*media.AudioFrame).Pts() != 11 {
		t.Fatalf("PTS = %d, want 11", output.(*media.AudioFrame).Pts())
	}
	assertEOF(t, item)
}

func TestTrimModeEndKeepsLeadingSilence(t *testing.T) {
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, TrimMode: config.TrimModeEnd, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 10, []float32{0, .1, 0, 0}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	assertSamples(t, output, []float32{0, .1})
	if output.(*media.AudioFrame).Pts() != 10 {
		t.Fatalf("PTS = %d, want 10", output.(*media.AudioFrame).Pts())
	}
	assertEOF(t, item)
}

func TestTrimReplaysExactBufferedSilenceByDefault(t *testing.T) {
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, TrimMode: config.TrimModeBoth, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.1, .05}))
	assertSamples(t, receive(t, item), []float32{.1})

	send(t, item, frame(48000, 2, []float32{0, 0}))
	if _, err := item.ReceiveFrame(); err != engine.ErrEAGAIN {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}

	send(t, item, frame(48000, 4, []float32{.2}))
	assertSamples(t, receive(t, item), []float32{.05})
	assertSamples(t, receive(t, item), []float32{0, 0})
	assertSamples(t, receive(t, item), []float32{.2})

	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestTrimApproximateSilenceReplaysZerosInsteadOfBufferedSamples(t *testing.T) {
	item, err := trim.New(config.TrimConfig{ThresholdDBFS: -20, TrimMode: config.TrimModeBoth, ApproximateSilence: true, MemoryLimitBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.1, .05}))
	assertSamples(t, receive(t, item), []float32{.1})

	send(t, item, frame(48000, 2, []float32{0, 0}))
	if _, err := item.ReceiveFrame(); err != engine.ErrEAGAIN {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}

	send(t, item, frame(48000, 4, []float32{.2}))
	assertSamples(t, receive(t, item), []float32{0}) // buffered .05 sample replays as digital silence, not its original value
	assertSamples(t, receive(t, item), []float32{0, 0})
	assertSamples(t, receive(t, item), []float32{.2})

	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestCompressorReducesGainAboveThresholdInstantly(t *testing.T) {
	item, err := compressor.New(config.CompressorConfig{ThresholdDBFS: -6, Ratio: 4, KneeDB: 0})
	if err != nil {
		t.Fatal(err)
	}
	overshoot := 0.0 - (-6.0)
	wantGain := float32(math.Pow(10, overshoot*(1.0/4-1)/20))
	send(t, item, frame(48000, 0, []float32{1, 1, 1, 1}))
	assertSamplesTol(t, receive(t, item), []float32{wantGain, wantGain, wantGain, wantGain}, 1e-4)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestCompressorPassesSignalBelowThresholdUnchanged(t *testing.T) {
	item, err := compressor.New(config.CompressorConfig{ThresholdDBFS: -6, Ratio: 4, KneeDB: 0})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.1, .1}))
	assertSamplesTol(t, receive(t, item), []float32{.1, .1}, 1e-5)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestEQPeakingZeroGainIsIdentity(t *testing.T) {
	item, err := eq.New(config.EQConfig{Type: config.EQTypePeaking, FrequencyHz: 1000, Q: 0.7071067811865476})
	if err != nil {
		t.Fatal(err)
	}
	values := []float32{0.2, -0.5, 0.8, -0.1, 0.05, 0.9}
	send(t, item, frame(48000, 0, values))
	assertSamplesTol(t, receive(t, item), values, 1e-4)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestGateSilencesSamplesBelowThreshold(t *testing.T) {
	item, err := gate.New(config.GateConfig{ThresholdDBFS: -20, GateMode: config.GateModeHard})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.5, .05, -.5, .02}))
	assertSamples(t, receive(t, item), []float32{.5, 0, -.5, 0})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestGateLinksChannelsSoAnyActiveChannelPreservesAll(t *testing.T) {
	item, err := gate.New(config.GateConfig{ThresholdDBFS: -20, GateMode: config.GateModeHard})
	if err != nil {
		t.Fatal(err)
	}
	block := audio.Block{Channels: [][]float32{{.5, .01}, {.01, .01}}, Layout: media.LayoutStereo2_0, Rate: 48000}
	encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
	if err != nil {
		t.Fatal(err)
	}
	var input media.Frame = encoded
	if err := item.SendFrame(&input); err != nil {
		t.Fatal(err)
	}
	input.Release()
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	decoded, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, decoded.Channels[0][0], .5)
	assertFloat(t, decoded.Channels[1][0], .01)
	assertFloat(t, decoded.Channels[0][1], 0)
	assertFloat(t, decoded.Channels[1][1], 0)
	output.Release()
	assertEOF(t, item)
}

func TestGateLowpassSilencesQuietSignalBelowRange(t *testing.T) {
	item, err := gate.New(config.GateConfig{
		ThresholdDBFS: -20, GateMode: config.GateModeLowpass, RangeDB: 40,
		OpenFrequencyHz: 20000, CloseFrequencyHz: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{.0001, .0001, .0001}))
	assertSamples(t, receive(t, item), []float32{0, 0, 0})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestGateLowpassSettlesToInputWhenFullyOpen(t *testing.T) {
	item, err := gate.New(config.GateConfig{
		ThresholdDBFS: -20, GateMode: config.GateModeLowpass, RangeDB: 40,
		OpenFrequencyHz: 20000, CloseFrequencyHz: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := make([]float32, 200)
	for i := range values {
		values[i] = 1
	}
	send(t, item, frame(48000, 0, values))
	output := receive(t, item)
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	if last := block.Channels[0][len(block.Channels[0])-1]; math.Abs(float64(last-1)) > 1e-3 {
		t.Fatalf("settled output = %g, want ~1", last)
	}
	output.Release()
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestReverbWetZeroIsIdentity(t *testing.T) {
	item, err := reverb.New(config.ReverbConfig{RoomSize: .5, Damping: .5, WetLevel: 0, DryLevel: 1})
	if err != nil {
		t.Fatal(err)
	}
	values := []float32{0.2, -0.5, 0.8, -0.1, 0.05, 0.9}
	send(t, item, frame(48000, 0, values))
	assertSamples(t, receive(t, item), values)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestReverbSilenceRemainsSilence(t *testing.T) {
	item, err := reverb.New(config.DefaultReverbConfig)
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{0, 0, 0, 0}))
	assertSamples(t, receive(t, item), []float32{0, 0, 0, 0})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestReverbRejectsSampleRateChangeMidStream(t *testing.T) {
	item, err := reverb.New(config.DefaultReverbConfig)
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(48000, 0, []float32{0}))
	receive(t, item).Release()
	input := frame(44100, 1, []float32{0})
	if err := item.SendFrame(&input); err == nil {
		t.Fatal("SendFrame with a different sample rate = nil error, want an error")
	}
	input.Release()
}

func TestReverbLowSampleRateDoesNotPanic(t *testing.T) {
	item, err := reverb.New(config.DefaultReverbConfig)
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(1, 0, []float32{1, 0, 0, 0}))
	output := receive(t, item)
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range block.Channels[0] {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("output contains non-finite sample: %v", block.Channels[0])
		}
	}
	output.Release()
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

func assertSamplesTol(t *testing.T, output media.Frame, want []float32, tol float64) {
	t.Helper()
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Channels) != 1 || len(block.Channels[0]) != len(want) {
		t.Fatalf("samples = %v, want %v", block.Channels, want)
	}
	for i := range want {
		if math.Abs(float64(block.Channels[0][i]-want[i])) > tol {
			t.Fatalf("sample[%d] = %g, want %g (tol %g)", i, block.Channels[0][i], want[i], tol)
		}
	}
	output.Release()
}
func assertEOF(t *testing.T, item engine.FilterEngine) {
	t.Helper()
	if _, err := item.ReceiveFrame(); err != engine.ErrEOF {
		t.Fatalf("ReceiveFrame() error = %v, want EOF", err)
	}
}
