package filter

import (
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/compressor"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/convolver"
	"github.com/godexture/filter-audio/internal/delay"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/gate"
	"github.com/godexture/filter-audio/internal/mixer"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
	"github.com/godexture/filter-audio/internal/retime"
	"github.com/godexture/filter-audio/internal/reverb"
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
	item, err := retime.New(config.SpeedConfig{Factor: 2, Mode: config.SpeedModeInterpolate})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	output := receive(t, item)
	if output.(*media.AudioFrame).SampleRate != 4 {
		t.Fatalf("SampleRate = %d, want 4 (unchanged)", output.(*media.AudioFrame).SampleRate)
	}
	assertSamples(t, output, []float32{0, 4})
	assertEOF(t, item)
}

func TestSpeedHalvesRateLengthensOutput(t *testing.T) {
	item, err := retime.New(config.SpeedConfig{Factor: 0.5, Mode: config.SpeedModeInterpolate})
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
	item, err := retime.New(config.SpeedConfig{Factor: 1, Mode: config.SpeedModeInterpolate})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	if output.(*media.AudioFrame).SampleRate != 4 {
		t.Fatalf("SampleRate = %d, want 4 (unchanged)", output.(*media.AudioFrame).SampleRate)
	}
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestSpeedRelabelDoublesRateLossless(t *testing.T) {
	item, err := retime.New(config.SpeedConfig{Factor: 2, Mode: config.SpeedModeRelabel})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	if output.(*media.AudioFrame).SampleRate != 8 {
		t.Fatalf("SampleRate = %d, want 8", output.(*media.AudioFrame).SampleRate)
	}
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestSpeedRelabelHalvesRateLossless(t *testing.T) {
	item, err := retime.New(config.SpeedConfig{Factor: 0.5, Mode: config.SpeedModeRelabel})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(4, 0, []float32{0, 2, 4, 6}))
	output := receive(t, item)
	if output.(*media.AudioFrame).SampleRate != 2 {
		t.Fatalf("SampleRate = %d, want 2", output.(*media.AudioFrame).SampleRate)
	}
	assertSamples(t, output, []float32{0, 2, 4, 6})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestSpeedRelabelPTSAdvancesBySampleCount(t *testing.T) {
	item, err := retime.New(config.SpeedConfig{Factor: 100, Mode: config.SpeedModeRelabel})
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
	if output.(*media.AudioFrame).Pts() != 10 {
		t.Fatalf("PTS = %d, want 10", output.(*media.AudioFrame).Pts())
	}
	assertSamples(t, output, []float32{0, .5, 1, .5})
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
	if output.(*media.AudioFrame).Pts() != 11 {
		t.Fatalf("PTS = %d, want 11", output.(*media.AudioFrame).Pts())
	}
	assertSamples(t, output, []float32{.1})
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
	if output.(*media.AudioFrame).Pts() != 11 {
		t.Fatalf("PTS = %d, want 11", output.(*media.AudioFrame).Pts())
	}
	assertSamples(t, output, []float32{.1, 0, 0})
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
	if output.(*media.AudioFrame).Pts() != 10 {
		t.Fatalf("PTS = %d, want 10", output.(*media.AudioFrame).Pts())
	}
	assertSamples(t, output, []float32{0, .1})
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

func TestDelayRepeatsInputAfterDelaySamples(t *testing.T) {
	item, err := delay.New(config.DelayConfig{DelayMs: 250, Feedback: 0, WetLevel: 1, DryLevel: 0})
	if err != nil {
		t.Fatal(err)
	}
	// rate 8, delayMs 250 -> exactly 2 samples of delay.
	send(t, item, frame(8, 0, []float32{1, 0, 0, 0, 0}))
	assertSamples(t, receive(t, item), []float32{0, 0, 1, 0, 0})
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestDelayFeedbackProducesDecayingRepeats(t *testing.T) {
	item, err := delay.New(config.DelayConfig{DelayMs: 250, Feedback: 0.5, WetLevel: 1, DryLevel: 0})
	if err != nil {
		t.Fatal(err)
	}
	send(t, item, frame(8, 0, []float32{1, 0, 0, 0, 0, 0}))
	assertSamplesTol(t, receive(t, item), []float32{0, 0, 1, 0, 0.5, 0}, 1e-6)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestDelayWetZeroIsIdentity(t *testing.T) {
	item, err := delay.New(config.DelayConfig{DelayMs: 250, Feedback: 0.3, WetLevel: 0, DryLevel: 1})
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

func TestDelayRejectsSampleRateChangeMidStream(t *testing.T) {
	item, err := delay.New(config.DefaultDelayConfig)
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

func TestMixerWrapFilterEndToEnd(t *testing.T) {
	adapter, err := mixer.New(2, 1, [][]float64{{1, 1}}, false)
	if err != nil {
		t.Fatal(err)
	}

	in0 := pipeline.NewChanEdge[media.Frame](2)
	in1 := pipeline.NewChanEdge[media.Frame](2)
	out := pipeline.NewChanEdge[media.Frame](2)
	adapter.InputPorts()["in0"].Connect(in0)
	adapter.InputPorts()["in1"].Connect(in1)
	adapter.OutputPorts()["out0"].Connect(out)

	if err := in0.Push(context.Background(), frame(48000, 0, []float32{0.1, 0.2})); err != nil {
		t.Fatal(err)
	}
	if err := in1.Push(context.Background(), frame(48000, 0, []float32{1, 1})); err != nil {
		t.Fatal(err)
	}
	in0.Close()
	in1.Close()

	if err := adapter.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	got, err := out.Pull(context.Background())
	if err != nil {
		t.Fatalf("out Pull() error = %v", err)
	}
	assertSamplesTol(t, got, []float32{1.1, 1.2}, 1e-6)

	if _, err := out.Pull(context.Background()); err != io.EOF {
		t.Fatalf("out Pull() error = %v, want EOF", err)
	}
}

func TestConvolveIdentityImpulseIsPassthrough(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{{1}},
		WetDryMix:       1,
		BlockSize:       8,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)
	input := []float32{0.1, -0.2, 0.3, -0.4, 0.5, -0.6, 0.7, -0.8}
	send(t, item, frame(48000, 0, input))
	assertSamplesTol(t, receive(t, item), input, 1e-4)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestConvolveMatchesDirectConvolution(t *testing.T) {
	const hop = 4
	ir := []float32{0.5, 0.3, -0.2, 0.1, 0.05, -0.05, 0.02, 0.01, -0.01} // len 9 = 2*hop+1 -> 3 partitions
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{ir},
		WetDryMix:       1,
		Normalize:       false,
		BlockSize:       hop,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)

	input := make([]float32, 20) // multiple of hop, split across uneven SendFrame calls below
	for i := range input {
		input[i] = float32(math.Sin(float64(i) * 0.7))
	}
	send(t, item, frame(48000, 0, input[:7]))
	send(t, item, frame(48000, 7, input[7:]))
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}

	var got []float32
	for {
		output, err := item.ReceiveFrame()
		if err != nil {
			break
		}
		block, err := audio.Decode(&output)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, block.Channels[0]...)
		output.Release()
	}

	want := directConvolution(input, ir)
	if len(got) != len(want) {
		t.Fatalf("output length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if diff := math.Abs(float64(got[i] - want[i])); diff > 1e-4 {
			t.Fatalf("sample[%d] = %g, want %g (diff %g)", i, got[i], want[i], diff)
		}
	}
}

func TestConvolvePreparesImpulseWithSharedPool(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{{1, 0, 0, 0, 0, 0, 0, 0, 0}},
		WetDryMix:       1,
		BlockSize:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	pool := registry.NewWorkerPool(2)
	t.Cleanup(func() { _ = pool.Close() })
	if err := item.Prepare(registry.ResourceGrant{Pool: pool}); err != nil {
		t.Fatal(err)
	}
	input := []float32{0.1, -0.2, 0.3, -0.4}
	send(t, item, frame(48000, 0, input))
	assertSamplesTol(t, receive(t, item), input, 1e-4)
}

func TestConvolveWetDryMixZeroIsDry(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{{0.5, 0.3, -0.2, 0.1}},
		WetDryMix:       0,
		BlockSize:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)
	input := []float32{0.1, 0.2, 0.3, 0.4}
	send(t, item, frame(48000, 0, input))
	assertSamplesTol(t, receive(t, item), input, 1e-4)
}

func TestConvolveMonoImpulseBroadcastsToEveryChannel(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{{1}},
		WetDryMix:       1,
		BlockSize:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)
	left := []float32{0.1, 0.2, 0.3, 0.4}
	right := []float32{-0.1, -0.2, -0.3, -0.4}
	send(t, item, multiFrame(48000, 0, media.LayoutStereo2_0, [][]float32{left, right}))
	output := receive(t, item)
	block, err := audio.Decode(&output)
	if err != nil {
		t.Fatal(err)
	}
	assertFloatSlice(t, block.Channels[0], left)
	assertFloatSlice(t, block.Channels[1], right)
	output.Release()
}

func TestConvolveUsesPortImpulseResponse(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{WetDryMix: 1, BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)
	ir := frame(48000, 0, []float32{1})
	if err := item.SendInput("ir", &ir); err != nil {
		t.Fatal(err)
	}
	ir.Release()
	if err := item.EndInput("ir"); err != nil {
		t.Fatal(err)
	}
	input := []float32{0.1, -0.2, 0.3, -0.4}
	send(t, item, frame(48000, 0, input))
	assertSamplesTol(t, receive(t, item), input, 1e-4)
}

func TestConvolvePerChannelImpulseRequiresMatchingChannelCount(t *testing.T) {
	item, err := convolver.New(config.ConvolutionConfig{
		ImpulseResponse: [][]float32{{1}, {1}, {1}}, // 3 channels, input below has 2
		WetDryMix:       1,
		BlockSize:       4,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareConvolve(t, item)
	input := multiFrame(48000, 0, media.LayoutStereo2_0, [][]float32{{0.1, 0.2, 0.3, 0.4}, {0.1, 0.2, 0.3, 0.4}})
	if err := item.SendFrame(&input); err == nil {
		t.Fatal("want error for impulse response channel count mismatch")
	}
	input.Release()
}

func directConvolution(x, h []float32) []float32 {
	y := make([]float32, len(x)+len(h)-1)
	for n := range y {
		var sum float32
		for k := 0; k < len(h); k++ {
			if n-k >= 0 && n-k < len(x) {
				sum += h[k] * x[n-k]
			}
		}
		y[n] = sum
	}
	return y
}

func prepareConvolve(t *testing.T, item *convolver.Engine) {
	t.Helper()
	if err := item.Prepare(registry.ResourceGrant{}); err != nil {
		t.Fatal(err)
	}
}

func assertFloatSlice(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		assertFloat(t, got[i], want[i])
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

func multiFrame(rate int, pts media.Pts, layout media.ChannelLayout, channels [][]float32) media.Frame {
	block := audio.Block{Channels: channels, Layout: layout, Rate: rate, PTS: pts}
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
	return output
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
