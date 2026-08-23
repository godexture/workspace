package integration_test

import (
	"strconv"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/testkit"
)

// The canonical representations wider than int16 each get their own codec
// component, so each one has to carry a stream on its own. The wire codings
// below are the ones that widen into them.
func runWideCodingCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	runCodingCase(t, set, coverage, sample.S24, sample.BigEndian,
		[]byte{0x80, 0x00, 0x00, 0x7f, 0xff, 0xff},
		[][]int32{{-2147483648, 2147483392}})
	runCodingCase(t, set, coverage, sample.S32, sample.LittleEndian,
		[]byte{0x00, 0x00, 0x00, 0x80, 0xff, 0xff, 0xff, 0x7f},
		[][]int32{{-2147483648, 2147483647}})
	runCodingCase(t, set, coverage, sample.F32, sample.LittleEndian,
		[]byte{0x00, 0x00, 0x80, 0xbf, 0x00, 0x00, 0x80, 0x3f},
		[][]float32{{-1, 1}})
	runCodingCase(t, set, coverage, sample.F64, sample.BigEndian,
		[]byte{
			0xbf, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
		[][]float64{{-1, 1}})
}

func runCodingCase[S audio.Sample](t *testing.T, set plugin.Set, coverage *testkit.Coverage, coding sample.Coding, endian sample.Endian, encoded []byte, planes [][]S) {
	t.Helper()
	wire := sample.Description{
		Coding: coding, Packing: sample.Interleaved, Endian: endian,
		Rate: 48_000, Layout: sample.Mono(), ValidBits: coding.Bits(),
	}
	planar := wire.Decoded()
	patch := linearPatch(wire, len(planes[0]))
	name := string(coding) + "-" + string(endian)

	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.DecoderIdentity(coding), "packets", codec.Packets(), "frames", sample.Frames[S]()), coverage),
		testkit.Case[packet.Packet, audio.Frame[S]]{
			Name:   name + "-decode",
			Config: patch,
			Input: testkit.PacketInput(wire, []testkit.Packet{{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(),
				Duration: timing.SomeDuration(timing.NewDuration(int64(len(planes[0])))), Bytes: encoded,
			}}),
			Want: testkit.WantFrames(testkit.Frame[S]{PTS: timing.SomePTS(timing.NewPTS(0)), Planes: planes}),
		},
	)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.EncoderIdentity(coding), "frames", sample.Frames[S](), "packets", codec.Packets()), coverage),
		testkit.Case[audio.Frame[S], packet.Packet]{
			Name:   name + "-encode",
			Config: patch,
			Input:  testkit.FrameInput(planar, []testkit.Frame[S]{{PTS: timing.SomePTS(timing.NewPTS(0)), Planes: planes}}),
			Want: testkit.WantPackets(testkit.Packet{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.SomeDTS(timing.NewDTS(0)),
				Duration: timing.SomeDuration(timing.NewDuration(int64(len(planes[0])))), Bytes: encoded,
			}),
		},
	)
	runWideSuggestions[S](t, set, coverage, coding, endian)
}

// Each wide codec answers from the stream it was handed, the same contract the
// s16 pair carries. Only the coding it reports differs.
func runWideSuggestions[S audio.Sample](t *testing.T, set plugin.Set, coverage *testkit.Coverage, coding sample.Coding, endian sample.Endian) {
	t.Helper()
	wire := sample.Description{
		Coding: coding, Packing: sample.Interleaved, Endian: endian,
		Rate: 48_000, Layout: sample.Mono(), ValidBits: coding.Bits(),
	}
	want := []testkit.Candidate{{
		"rate": "48000", "coding": string(coding), "validBits": strconv.Itoa(coding.Bits()),
		"layout": "FC", "endian": string(endian),
	}}
	if endian == sample.NoEndian {
		want[0]["endian"] = string(sample.LittleEndian)
	}
	decode := []testkit.Suggestion{{
		Name:    "follows-the-inspected-wire",
		Inputs:  flow.NewDescriptors(flow.Describe("packets", linearDescriptor(t, wire))),
		Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand("frames", plugin.ConditionNeed[stream.Descriptor]("linear.config"))},
		Want:    want,
	}}
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, linear.DecoderIdentity(coding), "packets", codec.Packets(), "frames", sample.Frames[S]()), coverage), decode...)

	encode := []testkit.Suggestion{{
		Name:    "follows-the-requested-wire",
		Inputs:  flow.NewDescriptors(flow.Describe("frames", linearFrameDescriptor[S](t, wire.Decoded()))),
		Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand("packets", plugin.DescriptorNeed("linear.config", linearDescriptor(t, wire)))},
		Want:    want,
	}}
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, linear.EncoderIdentity(coding), "frames", sample.Frames[S](), "packets", codec.Packets()), coverage), encode...)
}

func linearFrameDescriptor[S audio.Sample](t *testing.T, description sample.Description) stream.Descriptor {
	t.Helper()
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor("linear", sample.Frames[S]().Descriptor(), timing.MustBase(1, int64(description.Rate)), properties)
}
