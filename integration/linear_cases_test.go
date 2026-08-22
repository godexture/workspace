package integration_test

import (
	"strconv"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/testkit"
)

func runLinearCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	wire := sample.Description{
		Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000,
		Layout: sample.Mono, Endian: sample.LittleEndian,
	}
	planar := wire
	planar.Format = sample.S16Planar
	planar.Endian = sample.NoEndian
	patch := linearPatch(wire, 2)
	raw := []byte{0xf0, 0xff, 0x10, 0x00, 0x00, 0x80, 0xf0, 0x7f}
	first := []byte{0xf0, 0xff, 0x10, 0x00}
	second := []byte{0x00, 0x80, 0xf0, 0x7f}

	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, linear.ReaderIdentity(), "bytes", access.Bytes(), "chunks", mediaformat.Chunks()), coverage),
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:   "carrier-reframing",
			Config: patch,
			Input:  testkit.ByteInput(raw),
			Want: testkit.WantChunks(
				testkit.Chunk{Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.SomeDTS(timing.NewDTS(0)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: first},
				testkit.Chunk{Sequence: 1, PTS: timing.SomePTS(timing.NewPTS(2)), DTS: timing.SomeDTS(timing.NewDTS(2)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: second},
			),
		},
	)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.ParserIdentity(), "chunks", mediaformat.Chunks(), "packets", codec.Packets()), coverage),
		testkit.Case[packet.Chunk, packet.Packet]{
			Name:   "known-duration-and-dts-are-preserved",
			Config: patch,
			Input: testkit.ChunkInput(wire, []testkit.Chunk{
				{Sequence: 7, PTS: timing.SomePTS(timing.NewPTS(4)), DTS: timing.SomeDTS(timing.NewDTS(3)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: first},
				{Sequence: 8, PTS: timing.SomePTS(timing.NewPTS(6)), DTS: timing.SomeDTS(timing.NewDTS(5)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: second},
			}),
			Want: testkit.WantPackets(
				testkit.Packet{Sequence: 7, PTS: timing.SomePTS(timing.NewPTS(4)), DTS: timing.SomeDTS(timing.NewDTS(3)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: first},
				testkit.Packet{Sequence: 8, PTS: timing.SomePTS(timing.NewPTS(6)), DTS: timing.SomeDTS(timing.NewDTS(5)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: second},
			),
		},
		testkit.Case[packet.Chunk, packet.Packet]{
			Name:   "unknown-duration-and-dts-are-inferred-from-payload-and-pts",
			Config: patch,
			Input: testkit.ChunkInput(wire, []testkit.Chunk{
				{Sequence: 9, PTS: timing.SomePTS(timing.NewPTS(10)), DTS: timing.UnknownDTS(), Duration: timing.UnknownDuration(), Bytes: first},
				{Sequence: 11, PTS: timing.UnknownPTS(), DTS: timing.UnknownDTS(), Duration: timing.UnknownDuration(), Bytes: second},
			}),
			Want: testkit.WantPackets(
				testkit.Packet{Sequence: 9, PTS: timing.SomePTS(timing.NewPTS(10)), DTS: timing.SomeDTS(timing.NewDTS(10)), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: first},
				testkit.Packet{Sequence: 11, PTS: timing.UnknownPTS(), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: second},
			),
		},
		testkit.Case[packet.Chunk, packet.Packet]{
			Name:   "mismatched-duration-is-rejected",
			Config: patch,
			Input: testkit.ChunkInput(wire, []testkit.Chunk{{
				Sequence: 10, PTS: timing.SomePTS(timing.NewPTS(12)), DTS: timing.SomeDTS(timing.NewDTS(12)), Duration: timing.SomeDuration(timing.NewDuration(7)), Bytes: first,
			}}),
			Want: testkit.WantRunError[packet.Packet](linear.ErrDurationMismatch),
		},
	)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.DecoderIdentity(), "packets", codec.Packets(), "frames", sample.S16()), coverage),
		testkit.Case[packet.Packet, audio.Frame[int16]]{
			Name:   "twelve-bit-left-justified",
			Config: patch,
			Input: testkit.PacketInput(wire, []testkit.Packet{{
				Sequence: 3, PTS: timing.SomePTS(timing.NewPTS(9)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(4)), Bytes: raw,
			}}),
			Want: testkit.WantFrames(testkit.Frame{
				PTS: timing.SomePTS(timing.NewPTS(9)), Planes: [][]int16{{-1, 1, -2048, 2047}},
			}),
		},
		decoderBigEndianCase(),
	)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.EncoderIdentity(), "frames", sample.S16(), "packets", codec.Packets()), coverage),
		testkit.Case[audio.Frame[int16], packet.Packet]{
			Name:   "twelve-bit-left-justify",
			Config: patch,
			Input: testkit.FrameInput(planar, []testkit.Frame{{
				PTS: timing.SomePTS(timing.NewPTS(9)), Planes: [][]int16{{-1, 1, -2048, 2047}},
			}}),
			Want: testkit.WantPackets(testkit.Packet{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(9)), DTS: timing.SomeDTS(timing.NewDTS(9)), Duration: timing.SomeDuration(timing.NewDuration(4)), Bytes: raw,
			}),
		},
		testkit.Case[audio.Frame[int16], packet.Packet]{
			Name:   "unknown-pts-keeps-unknown-dts",
			Config: patch,
			Input: testkit.FrameInput(planar, []testkit.Frame{{
				PTS: timing.UnknownPTS(), Planes: [][]int16{{0, 0}},
			}}),
			Want: testkit.WantPackets(testkit.Packet{
				Sequence: 0, PTS: timing.UnknownPTS(), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: []byte{0, 0, 0, 0},
			}),
		},
		encoderBigEndianCase(),
	)
	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, linear.WriterIdentity(), "packets", codec.Packets(), "writes", access.Writes()), coverage),
		testkit.Case[packet.Packet, access.Write]{
			Name:   "packet-payload-move",
			Config: patch,
			Input: testkit.PacketInput(wire, []testkit.Packet{{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(4)), Bytes: raw,
			}}),
			Want: testkit.WantWrites(testkit.Write{Operation: access.AppendOperation, Bytes: raw}),
		},
	)
}

func decoderBigEndianCase() testkit.Case[packet.Packet, audio.Frame[int16]] {
	wire := sample.Description{
		Format: sample.S16Interleaved, ValidBits: 16, Rate: 44_100,
		Layout: sample.Stereo, Endian: sample.BigEndian,
	}
	return testkit.Case[packet.Packet, audio.Frame[int16]]{
		Name:   "sixteen-bit-big-endian-stereo",
		Config: linearPatch(wire, 3),
		Input: testkit.PacketInput(wire, []testkit.Packet{{
			Sequence: 4, PTS: timing.SomePTS(timing.NewPTS(2)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)),
			Bytes: []byte{0x80, 0x00, 0x7f, 0xff, 0x00, 0x00, 0xff, 0xff},
		}}),
		Want: testkit.WantFrames(testkit.Frame{
			PTS: timing.SomePTS(timing.NewPTS(2)), Planes: [][]int16{{-32768, 0}, {32767, -1}},
		}),
	}
}

func encoderBigEndianCase() testkit.Case[audio.Frame[int16], packet.Packet] {
	wire := sample.Description{
		Format: sample.S16Interleaved, ValidBits: 16, Rate: 44_100,
		Layout: sample.Stereo, Endian: sample.BigEndian,
	}
	planar := wire
	planar.Format = sample.S16Planar
	planar.Endian = sample.NoEndian
	return testkit.Case[audio.Frame[int16], packet.Packet]{
		Name:   "sixteen-bit-big-endian-stereo",
		Config: linearPatch(wire, 3),
		Input: testkit.FrameInput(planar, []testkit.Frame{{
			PTS: timing.SomePTS(timing.NewPTS(2)), Planes: [][]int16{{-32768, 0}, {32767, -1}},
		}}),
		Want: testkit.WantPackets(testkit.Packet{
			Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(2)), DTS: timing.SomeDTS(timing.NewDTS(2)), Duration: timing.SomeDuration(timing.NewDuration(2)),
			Bytes: []byte{0x80, 0x00, 0x7f, 0xff, 0x00, 0x00, 0xff, 0xff},
		}),
	}
}

func linearPatch(description sample.Description, chunkSamples int) config.Patch {
	return config.NewPatch().
		SetText("rate", strconv.Itoa(description.Rate)).
		SetText("validBits", strconv.Itoa(description.ValidBits)).
		SetText("layout", string(description.Layout)).
		SetText("endian", string(description.Endian)).
		SetText("chunkSamples", strconv.Itoa(chunkSamples))
}

// Every linear component declares Suggest with a limit of one. The planner
// only ever asks it to reconcile an inspected input with an optional target,
// so the contract worth pinning is that it answers from the input, follows an
// explicit endian request, and refuses a format it cannot carry.
func runLinearSuggestions(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	input := sample.Description{
		Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000,
		Layout: sample.Stereo, Endian: sample.BigEndian,
	}

	for _, subject := range []struct {
		identity plugin.Identity
		input    string
		output   string
		run      func(*testing.T, plugin.Set, plugin.Identity, *testkit.Coverage, []testkit.Suggestion)
	}{
		{identity: linear.ReaderIdentity(), input: "bytes", output: "chunks", run: suggestBytesToChunks},
		{identity: linear.ParserIdentity(), input: "chunks", output: "packets", run: suggestChunksToPackets},
		{identity: linear.DecoderIdentity(), input: "packets", output: "frames", run: suggestPacketsToFrames},
		{identity: linear.EncoderIdentity(), input: "frames", output: "packets", run: suggestFramesToPackets},
		{identity: linear.WriterIdentity(), input: "packets", output: "writes", run: suggestPacketsToWrites},
	} {
		subject.run(t, set, subject.identity, coverage, linearSuggestionCases(t, subject.input, subject.output, subject.identity == linear.DecoderIdentity(), linearDescriptor(t, input)))
	}
}

// linearSuggestionCases demands the wire description on the port that carries
// it. Only a decoder reads interleaved samples and writes planar frames, so a
// demand on its output says nothing about the byte order it must read.
func linearSuggestionCases(t *testing.T, inputPort, outputPort string, wireIsInput bool, input stream.Descriptor) []testkit.Suggestion {
	t.Helper()
	wireDemand := func(need plugin.Need[stream.Descriptor]) plugin.Demand[stream.Descriptor] {
		if wireIsInput {
			return plugin.InputDemand(inputPort, need)
		}
		return plugin.OutputDemand(outputPort, need)
	}
	return []testkit.Suggestion{
		{
			Name:    "follows-the-inspected-input",
			Inputs:  flow.NewDescriptors(flow.Describe(inputPort, input)),
			Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand(outputPort, plugin.ConditionNeed[stream.Descriptor]("linear.config"))},
			Want: []testkit.Candidate{{
				"rate": "32000", "validBits": "12", "layout": "stereo", "endian": "big",
			}},
		},
		{
			Name:   "adopts-the-requested-endian",
			Inputs: flow.NewDescriptors(flow.Describe(inputPort, input)),
			Demands: []plugin.Demand[stream.Descriptor]{wireDemand(plugin.DescriptorNeed(
				"linear.config",
				linearDescriptor(t, sample.Description{
					Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000,
					Layout: sample.Stereo, Endian: sample.LittleEndian,
				}),
			))},
			Want: []testkit.Candidate{{
				"rate": "32000", "validBits": "12", "layout": "stereo", "endian": "little",
			}},
		},
		{
			Name:    "offers-nothing-without-sample-properties",
			Inputs:  flow.NewDescriptors(flow.Describe(inputPort, stream.MustDescriptor("opaque", codec.Packets().Descriptor(), timing.MustBase(1, 48_000), property.New()))),
			Demands: []plugin.Demand[stream.Descriptor]{plugin.OutputDemand(outputPort, plugin.ConditionNeed[stream.Descriptor]("linear.config"))},
		},
	}
}

func suggestBytesToChunks(t *testing.T, set plugin.Set, identity plugin.Identity, coverage *testkit.Coverage, suggestions []testkit.Suggestion) {
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, identity, "bytes", access.Bytes(), "chunks", mediaformat.Chunks()), coverage), suggestions...)
}

func suggestChunksToPackets(t *testing.T, set plugin.Set, identity plugin.Identity, coverage *testkit.Coverage, suggestions []testkit.Suggestion) {
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, identity, "chunks", mediaformat.Chunks(), "packets", codec.Packets()), coverage), suggestions...)
}

func suggestPacketsToFrames(t *testing.T, set plugin.Set, identity plugin.Identity, coverage *testkit.Coverage, suggestions []testkit.Suggestion) {
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, identity, "packets", codec.Packets(), "frames", sample.S16()), coverage), suggestions...)
}

func suggestFramesToPackets(t *testing.T, set plugin.Set, identity plugin.Identity, coverage *testkit.Coverage, suggestions []testkit.Suggestion) {
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, identity, "frames", sample.S16(), "packets", codec.Packets()), coverage), suggestions...)
}

func suggestPacketsToWrites(t *testing.T, set plugin.Set, identity plugin.Identity, coverage *testkit.Coverage, suggestions []testkit.Suggestion) {
	testkit.Suggests(t, testkit.Track(testkit.SubjectIn(set, identity, "packets", codec.Packets(), "writes", access.Writes()), coverage), suggestions...)
}

func linearDescriptor(t *testing.T, description sample.Description) stream.Descriptor {
	t.Helper()
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor("linear", codec.Packets().Descriptor(), timing.MustBase(1, int64(description.Rate)), properties)
}
