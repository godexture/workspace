package audio_test

import (
	"testing"

	"github.com/godexture/godec/config"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin/audio"
	"github.com/godexture/godec/testkit"
)

func processed(layout sample.Layout) sample.Description {
	return sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: layout, ValidBits: 32},
		Coding:  sample.F32,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
}

func filterSubject(name audio.Processor) testkit.Subject[mediaaudio.Frame[float32], mediaaudio.Frame[float32]] {
	frames := sample.Frames[float32]()
	return testkit.SubjectIn(audio.Set(), audio.ProcessorIdentity(name), "frames", frames, "filtered", frames)
}

// A gain is stated in decibels, so the samples it produces are the input times
// ten to a twentieth of that. Half and double are the two values where that
// definition lands exactly on a float32, which is what lets this compare
// samples rather than tolerances.
func TestGainScalesEverySampleByItsDecibels(t *testing.T) {
	pts := timing.SomePTS(timing.NewPTS(0))
	input := []testkit.Frame[float32]{{PTS: pts, Planes: [][]float32{{-1, 0, 0.25}, {1, 0.5, -0.75}}}}
	testkit.Codec(t, filterSubject(audio.Gain),
		testkit.Case[mediaaudio.Frame[float32], mediaaudio.Frame[float32]]{
			Name:   "halve",
			Config: config.NewPatch().SetText("decibels", "-6.020599913279624"),
			Input:  testkit.FrameInput(processed(sample.Stereo()), input),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{-0.5, 0, 0.125}, {0.5, 0.25, -0.375}},
			}),
		},
		testkit.Case[mediaaudio.Frame[float32], mediaaudio.Frame[float32]]{
			Name:   "unity",
			Config: config.NewPatch(),
			Input:  testkit.FrameInput(processed(sample.Stereo()), input),
			Want: testkit.WantFrames(testkit.Frame[float32]{
				PTS: pts, Planes: [][]float32{{-1, 0, 0.25}, {1, 0.5, -0.75}},
			}),
		},
	)
}

// A filter states the samples it reads and nothing else about them, so a
// stream stored some other way is one the planner bridges rather than one the
// filter refuses. Integral samples reach the gain through a converter and come
// back through another, and the halved values are what proves both ran: the
// filter itself never learned the stream was stored as integers.
func TestAFilterHasItsSamplesBridgedRatherThanRefusingThem(t *testing.T) {
	pts := timing.SomePTS(timing.NewPTS(0))
	integral := sample.Description{
		Signal:  sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 16},
		Coding:  sample.S16,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
	frames := sample.Frames[int16]()
	subject := testkit.SubjectIn(audio.Set(), audio.ProcessorIdentity(audio.Gain), "frames", frames, "filtered", frames)
	if subject.Identity().IsZero() {
		t.Fatal("gain is not in the family set")
	}
	testkit.Codec(t, subject,
		testkit.Case[mediaaudio.Frame[int16], mediaaudio.Frame[int16]]{
			Name:   "integral-samples",
			Config: config.NewPatch().SetText("decibels", "-6.020599913279624"),
			Input: testkit.FrameInput(integral, []testkit.Frame[int16]{{
				PTS: pts, Planes: [][]int16{{-32768, 0, 16384}},
			}}),
			Want: testkit.WantFrames(testkit.Frame[int16]{PTS: pts, Planes: [][]int16{{-16384, 0, 8192}}}),
		},
	)
}

// Multichannel is not a special case for a filter: the layout decides how many
// planes it walks and nothing else.
func TestAFilterWalksEveryChannelTheLayoutNames(t *testing.T) {
	pts := timing.SomePTS(timing.NewPTS(7))
	planes := [][]float32{{1, 1}, {0.5, 0.5}, {-1, -1}, {0.25, 0.25}, {0, 0}, {-0.5, -0.5}}
	want := [][]float32{{0.5, 0.5}, {0.25, 0.25}, {-0.5, -0.5}, {0.125, 0.125}, {0, 0}, {-0.25, -0.25}}
	testkit.Codec(t, filterSubject(audio.Gain),
		testkit.Case[mediaaudio.Frame[float32], mediaaudio.Frame[float32]]{
			Name:   "six-channels",
			Config: config.NewPatch().SetText("decibels", "-6.020599913279624"),
			Input:  testkit.FrameInput(processed(sample.Channels(6)), []testkit.Frame[float32]{{PTS: pts, Planes: planes}}),
			Want:   testkit.WantFrames(testkit.Frame[float32]{PTS: pts, Planes: want}),
		},
	)
}
