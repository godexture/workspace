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

// canonicalSignal is the same three points -- negative full scale, silence and
// half scale -- written in each canonical representation. A conversion has to
// map one spelling of it onto another, which is what makes the expectation a
// statement about the representations rather than about the implementation.
func canonicalSignal[S audio.Sample]() []S {
	var values any
	switch any(*new(S)).(type) {
	case int16:
		values = []int16{-32768, 0, 16384}
	case int32:
		values = []int32{-2147483648, 0, 1073741824}
	case float32:
		values = []float32{-1, 0, 0.5}
	case float64:
		values = []float64{-1, 0, 0.5}
	}
	typed, _ := values.([]S)
	return typed
}

func runConversionCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	runConversion[int16, int32](t, set, coverage)
	runConversion[int16, float32](t, set, coverage)
	runConversion[int16, float64](t, set, coverage)
	runConversion[int32, int16](t, set, coverage)
	runConversion[int32, float32](t, set, coverage)
	runConversion[int32, float64](t, set, coverage)
	runConversion[float32, int16](t, set, coverage)
	runConversion[float32, int32](t, set, coverage)
	runConversion[float32, float64](t, set, coverage)
	runConversion[float64, int16](t, set, coverage)
	runConversion[float64, int32](t, set, coverage)
	runConversion[float64, float32](t, set, coverage)
}

func runConversion[From, To audio.Sample](t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	from, to := sample.CodingOf[From](), sample.CodingOf[To]()
	pts := timing.SomePTS(timing.NewPTS(5))
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, pluginaudio.ConverterIdentity(from, to),
			"frames", sample.Frames[From](), "converted", sample.Frames[To]()), coverage),
		testkit.Case[audio.Frame[From], audio.Frame[To]]{
			Name:   string(from) + "-to-" + string(to),
			Config: config.NewPatch(),
			Input: testkit.FrameInput(planarDescription(from), []testkit.Frame[From]{{
				PTS: pts, Planes: [][]From{canonicalSignal[From]()},
			}}),
			Want: testkit.WantFrames(testkit.Frame[To]{PTS: pts, Planes: [][]To{canonicalSignal[To]()}}),
		},
	)
}

func planarDescription(coding sample.Coding) sample.Description {
	return sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: coding.Bits()},
		Coding:  coding,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
}
