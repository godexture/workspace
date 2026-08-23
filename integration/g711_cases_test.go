package integration_test

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	mediaschema "github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/g711"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/testkit"
)

// Companding is a table lookup in both directions, so what is worth pinning is
// the shape around it: the endpoints of each curve, that a decoded sample
// spans the whole container it is stored in, and that a stereo stream is
// deinterleaved by channel rather than by byte.
func runCompandedCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	signal := sample.Signal{Rate: 8_000, Layout: sample.Stereo()}
	decoded := sample.Description{
		Signal:  sample.Signal{Rate: 8_000, Layout: sample.Stereo(), ValidBits: 16},
		Coding:  sample.S16,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
	coded := []byte{0x00, 0x80, 0xff, 0x7f}

	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, g711.ParserIdentity(), "chunks", mediaformat.Chunks(), "packets", codec.Packets()), coverage),
		testkit.Case[packet.Chunk, packet.Packet]{
			Name:   "companded-chunk-carries-its-duration",
			Config: config.NewPatch(),
			Input: testkit.ChunkInputFor(signalDescriptor(t, signal, mediaformat.Chunks().Descriptor()), []testkit.Chunk{{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(), Bytes: coded,
			}}),
			Want: testkit.WantPackets(testkit.Packet{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.SomeDTS(timing.NewDTS(0)),
				Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: coded,
			}),
		},
	)

	// Both curves spell zero twice, so the code that expands to it is not the
	// code companding chooses to write back. Every other code round trips.
	for law, expected := range map[g711.Law]struct {
		planes    [][]int16
		rewritten []byte
	}{
		g711.ULaw: {[][]int16{{-32124, 0}, {32124, 0}}, []byte{0x00, 0x80, 0xff, 0xff}},
		g711.ALaw: {[][]int16{{-5504, 848}, {5504, -848}}, []byte{0x00, 0x80, 0xff, 0x7f}},
	} {
		testkit.Codec(t,
			testkit.Track(testkit.SubjectIn(set, g711.DecoderIdentity(law), "packets", codec.Packets(), "frames", sample.Frames[int16]()), coverage),
			testkit.Case[packet.Packet, audio.Frame[int16]]{
				Name:   law.String() + "-expands-to-full-scale",
				Config: config.NewPatch(),
				Input: testkit.PacketInputFor(signalDescriptor(t, signal, codec.Packets().Descriptor()), []testkit.Packet{{
					Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(),
					Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: coded,
				}}),
				Want: testkit.WantFrames(testkit.Frame[int16]{PTS: timing.SomePTS(timing.NewPTS(0)), Planes: expected.planes}),
			},
		)
		testkit.Codec(t,
			testkit.Track(testkit.SubjectIn(set, g711.EncoderIdentity(law), "frames", sample.Frames[int16](), "packets", codec.Packets()), coverage),
			testkit.Case[audio.Frame[int16], packet.Packet]{
				Name:   law.String() + "-compands-back-to-its-codes",
				Config: config.NewPatch(),
				Input:  testkit.FrameInput(decoded, []testkit.Frame[int16]{{PTS: timing.SomePTS(timing.NewPTS(0)), Planes: expected.planes}}),
				Want: testkit.WantPackets(testkit.Packet{
					Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.SomeDTS(timing.NewDTS(0)),
					Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: expected.rewritten,
				}),
			},
		)
	}
}

// signalDescriptor describes a stream that states what it is and not how its
// samples are stored, which is what a companded carrier publishes.
func signalDescriptor(t *testing.T, signal sample.Signal, schema mediaschema.Descriptor) stream.Descriptor {
	t.Helper()
	properties, err := signal.Properties()
	if err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor("companded", schema, timing.MustBase(1, int64(signal.Rate)), properties)
}

// A coder cannot know what its container calls it, so it takes the name from
// the stream the container asked for. Nothing else in its configuration comes
// from the graph, which is what keeps the law a property of the component.
func runCompandedSuggestions(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	signal := sample.Signal{Rate: 8_000, Layout: sample.Stereo()}
	decoded := sample.Description{
		Signal:  sample.Signal{Rate: 8_000, Layout: sample.Stereo(), ValidBits: 16},
		Coding:  sample.S16,
		Packing: sample.Planar,
		Endian:  sample.NoEndian,
	}
	named, err := codec.WithTag(mustProperties(t, signal), wave.ULawTag())
	if err != nil {
		t.Fatal(err)
	}
	wanted := stream.MustDescriptor("companded", codec.Packets().Descriptor(), timing.MustBase(1, 8_000), named)

	for _, law := range []g711.Law{g711.ALaw, g711.ULaw} {
		testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, g711.DecoderIdentity(law),
			"packets", codec.Packets(), "frames", sample.Frames[int16]()), coverage),
			testkit.Suggestion{
				Name:    "expansion-takes-nothing-from-the-graph",
				Inputs:  flow.NewDescriptors(flow.Describe("packets", signalDescriptor(t, signal, codec.Packets().Descriptor()))),
				Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand("frames", plugin.ConditionNeed[stream.Descriptor]("g711.config"))},
				Want:    []testkit.Candidate{{"chunkSamples": "1024", "tag": ""}},
			})
		testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, g711.EncoderIdentity(law),
			"frames", sample.Frames[int16](), "packets", codec.Packets()), coverage),
			testkit.Suggestion{
				Name:    "companding-is-named-by-its-container",
				Inputs:  flow.NewDescriptors(flow.Describe("frames", linearFrameDescriptor[int16](t, decoded))),
				Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand("packets", plugin.DescriptorNeed("wave.codec", wanted))},
				Want:    []testkit.Candidate{{"chunkSamples": "1024", "tag": wave.ULawTag().String()}},
			})
	}
}

func mustProperties(t *testing.T, signal sample.Signal) property.Set {
	t.Helper()
	properties, err := signal.Properties()
	if err != nil {
		t.Fatal(err)
	}
	return properties
}
