package integration_test

import (
	"strconv"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
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
				testkit.Chunk{Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), Bytes: first},
				testkit.Chunk{Sequence: 1, PTS: timing.SomePTS(timing.NewPTS(2)), Bytes: second},
			),
		},
	)
	testkit.Codec(t,
		testkit.Track(testkit.SubjectIn(set, linear.ParserIdentity(), "chunks", mediaformat.Chunks(), "packets", codec.Packets()), coverage),
		testkit.Case[packet.Chunk, packet.Packet]{
			Name:   "chunk-boundaries",
			Config: patch,
			Input: testkit.ChunkInput(wire, []testkit.Chunk{
				{Sequence: 7, PTS: timing.SomePTS(timing.NewPTS(4)), Bytes: first},
				{Sequence: 8, PTS: timing.SomePTS(timing.NewPTS(6)), Bytes: second},
			}),
			Want: testkit.WantPackets(
				testkit.Packet{Sequence: 7, PTS: timing.SomePTS(timing.NewPTS(4)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: first},
				testkit.Packet{Sequence: 8, PTS: timing.SomePTS(timing.NewPTS(6)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: second},
			),
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
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(9)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(4)), Bytes: raw,
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
			Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(2)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)),
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
