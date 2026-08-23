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
}
