package integration_test

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	pluginaudio "github.com/godexture/godec/plugin/audio"
	"github.com/godexture/godec/testkit"
)

func filterSubject(set plugin.Set, coverage *testkit.Coverage, name pluginaudio.Processor) testkit.Subject[audio.Frame[float32], audio.Frame[float32]] {
	frames := sample.Frames[float32]()
	return testkit.Track(testkit.SubjectIn(set, pluginaudio.ProcessorIdentity(name),
		"frames", frames, "filtered", frames), coverage)
}

func processedDescription(layout sample.Layout) sample.Description {
	return sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: layout, ValidBits: 32},
		Coding:  sample.F32,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
}

func runFilterCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	pts := timing.SomePTS(timing.NewPTS(0))

	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Gain),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "gain-halves",
			Config: config.NewPatch().SetText("decibels", "-6.020599913279624"),
			Input: testkit.FrameInput(processedDescription(sample.Stereo()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{-1, 0.25}, {1, -0.5}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{-0.5, 0.125}, {0.5, -0.25}},
			}),
		},
	)

	// A DC blocker passes its first sample through untouched and works on the
	// difference from there, so a constant leaves exactly one sample standing.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.DCOffset),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "dc-offset-removes-a-constant",
			Config: config.NewPatch().SetText("pole", "0.5"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, 1, 1, 1}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{1, 0.5, 0.25, 0.125}},
			}),
		},
	)

	// Below the threshold a compressor is only its makeup gain, which is the
	// one part of the chain that stays exact whatever the detector is doing.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Compressor),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name: "compressor-passes-quiet-signals",
			Config: config.NewPatch().
				SetText("threshold", "-6.020599913279624").
				SetText("makeupGain", "-6.020599913279624").
				SetText("knee", "0"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{0.25, -0.125, 0.0625}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{0.125, -0.0625, 0.03125}},
			}),
		},
	)

	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Gate),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "gate-silences-below-its-threshold",
			Config: config.NewPatch().SetText("threshold", "-6.020599913279624"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, 0.25, -0.5}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{1, 0, -0.5}},
			}),
		},
	)

	// A band asking for no level change has to be exactly transparent: the
	// numerator and denominator of the section are the same polynomial, and
	// only a correctly normalised cascade shows that as identical samples.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Equalizer),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name: "equalizer-with-no-gain-is-transparent",
			Config: config.NewPatch().SetText("bands",
				`[{"type":"peaking","frequency":1000,"gain":0,"q":1},{"type":"lowshelf","frequency":200,"gain":0,"q":0.7}]`),
			Input: testkit.FrameInput(processedDescription(sample.Stereo()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, -0.5, 0.25}, {0, 0.75, -1}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{1, -0.5, 0.25}, {0, 0.75, -1}},
			}),
		},
	)

	// A delay of exactly one sample at this rate puts the repeat in the next
	// position, so the whole filter is visible in eight samples.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Delay),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name: "delay-repeats-a-sample",
			Config: config.NewPatch().
				SetText("time", "62.5us").
				SetText("feedback", "0").
				SetText("wet", "1").
				SetText("dry", "1"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, 0, 0, 0, 0, 0}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{1, 0, 0, 1, 0, 0}},
			}),
		},
	)

	// With no wet level a reverb is only its dry level, which is the one part
	// of a network of twelve delay lines that stays exact.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Reverb),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "reverb-without-wet-signal-only-scales",
			Config: config.NewPatch().SetText("wet", "0").SetText("dry", "0.5"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, -0.5, 0.25}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{0.5, -0.25, 0.125}},
			}),
		},
	)

	// A centre channel the target does not have is spread over its front pair,
	// and the stream comes out stated across two channels rather than one.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Remix),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name: "remix-folds-a-centre-into-a-pair",
			Config: config.NewPatch().
				SetText("layout", "stereo").
				SetText("center", "0").
				SetText("normalize", "false"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, -0.5, 0.25}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{1, -0.5, 0.25}, {1, -0.5, 0.25}},
			}),
		},
	)

	// Halving the rate reads a ramp at every other position, so eight samples
	// come back as the four the new rate falls on.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Resample),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "resample-halves-the-rate",
			Config: config.NewPatch().SetText("rate", "24000"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{0, 0.125, 0.25, 0.375, 0.5, 0.625, 0.75, 0.875}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{0, 0.25, 0.5, 0.75}},
			}),
		},
	)

	// Relabelling keeps every sample and only says they pass sooner, so it is
	// the one way of retiming that is exact.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Retime),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "retime-relabels-without-touching-samples",
			Config: config.NewPatch().SetText("factor", "2").SetText("mode", "relabel"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: timing.SomePTS(timing.NewPTS(8)), Planes: [][]float32{{1, -0.5, 0.25}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: timing.SomePTS(timing.NewPTS(16)), Planes: [][]float32{{1, -0.5, 0.25}},
			}),
		},
	)

	// Normalize emits nothing until the stream has ended, because the level it
	// applies is a fact about the loudest sample anywhere in it.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Normalize),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "normalize-brings-the-peak-to-the-target",
			Config: config.NewPatch().SetText("target", "0"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{
				{PTS: pts, Planes: [][]float32{{0.25, -0.5}}},
				{PTS: timing.SomePTS(timing.NewPTS(2)), Planes: [][]float32{{0.125, 0}}},
			}),
			Want: testkit.WantFrames(
				testkit.Frame[float32]{PTS: pts, Planes: [][]float32{{0.5, -1}}},
				testkit.Frame[float32]{PTS: timing.SomePTS(timing.NewPTS(2)), Planes: [][]float32{{0.25, 0}}},
			),
		},
	)

	// A fade-out needs the end of the stream, so the fixture states one; a
	// fade-in never does, because the start is where it already is.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Fade),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "fade-rises-from-silence-and-falls-back",
			Config: config.NewPatch().SetText("in", "83334ns").SetText("out", "83334ns"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{{
				PTS: pts, Planes: [][]float32{{1, 1, 1, 1, 1, 1, 1, 1}},
			}}, testkit.WithDuration(timing.NewDuration(8))),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{0, 0.25, 0.5, 0.75, 1, 0.75, 0.5, 0.25}},
			}),
		},
	)

	// A quiet run is the end of the stream until something else arrives, so
	// the gap in the middle survives and the run at the end does not.
	testkit.Codec(t, filterSubject(set, coverage, pluginaudio.Trim),
		testkit.Case[audio.Frame[float32], audio.Frame[float32]]{
			Name:   "trim-drops-the-silence-at-both-ends",
			Config: config.NewPatch().SetText("threshold", "-6.020599913279624"),
			Input: testkit.FrameInput(processedDescription(sample.Mono()), []testkit.Frame[float32]{
				{PTS: pts, Planes: [][]float32{{0, 1, 0, 0}}},
				{PTS: timing.SomePTS(timing.NewPTS(4)), Planes: [][]float32{{0, 0, 1, 0}}},
			}),
			Want: testkit.WantFrames(
				testkit.Frame[float32]{PTS: timing.SomePTS(timing.NewPTS(1)), Planes: [][]float32{{1}}},
				testkit.Frame[float32]{PTS: timing.SomePTS(timing.NewPTS(2)), Planes: [][]float32{{0, 0}}},
				testkit.Frame[float32]{PTS: timing.SomePTS(timing.NewPTS(4)), Planes: [][]float32{{0, 0, 1}}},
			),
		},
	)
}
