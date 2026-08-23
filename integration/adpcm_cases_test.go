package integration_test

import (
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	mediaschema "github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/adpcm"
	"github.com/godexture/godec/testkit"
)

// An ADPCM block restates the predictor state it starts from, so the samples a
// block header carries are decodable on their own. Those are what these cases
// pin: the block geometry the parameters state, and that the two seed samples
// come out oldest first.
func runADPCMCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	for _, testCase := range []struct {
		variant adpcm.Variant
		samples int
		block   []byte
		planes  [][]int16
	}{
		// Eight-byte blocks keep the expansion small enough to write out. A
		// zero nybble against the first coefficient pair repeats the predictor,
		// so every sample after the seeds holds.
		{adpcm.Microsoft, (8-7)*2 + 2, microsoftBlock(8), [][]int16{{500, 1000, 1000, 1000}}},
		{adpcm.IMA, (8-4)*2 + 1, imaBlock(8), [][]int16{{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}}},
	} {
		signal := sample.Signal{Rate: 48_000, Layout: sample.Mono()}
		descriptor := adpcmDescriptor(t, signal, testCase.variant, testCase.samples, codec.Packets().Descriptor())

		testkit.Codec(t,
			testkit.Track(testkit.SubjectIn(set, adpcm.ParserIdentity(testCase.variant), "chunks", mediaformat.Chunks(), "packets", codec.Packets()), coverage),
			testkit.Case[packet.Chunk, packet.Packet]{
				Name:   testCase.variant.String() + "-block-states-its-sample-count",
				Config: config.NewPatch(),
				Input: testkit.ChunkInputFor(adpcmDescriptor(t, signal, testCase.variant, testCase.samples, mediaformat.Chunks().Descriptor()), []testkit.Chunk{{
					Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(), Bytes: testCase.block,
				}}),
				Want: testkit.WantPackets(testkit.Packet{
					Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.SomeDTS(timing.NewDTS(0)),
					Duration: timing.SomeDuration(timing.NewDuration(int64(testCase.samples))), Bytes: testCase.block,
				}),
			},
		)
		testkit.Codec(t,
			testkit.Track(testkit.SubjectIn(set, adpcm.DecoderIdentity(testCase.variant), "packets", codec.Packets(), "frames", sample.Frames[int16]()), coverage),
			testkit.Case[packet.Packet, audio.Frame[int16]]{
				Name:   testCase.variant.String() + "-seeds-from-its-block-header",
				Config: config.NewPatch(),
				Input: testkit.PacketInputFor(descriptor, []testkit.Packet{{
					Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(),
					Duration: timing.SomeDuration(timing.NewDuration(int64(testCase.samples))), Bytes: testCase.block,
				}}),
				Want: testkit.WantFrames(testkit.Frame[int16]{PTS: timing.SomePTS(timing.NewPTS(0)), Planes: testCase.planes}),
			},
		)
	}
}

func microsoftBlock(align int) []byte {
	block := make([]byte, align)
	binary.LittleEndian.PutUint16(block[1:3], 16)
	binary.LittleEndian.PutUint16(block[3:5], uint16(int16(1000)))
	binary.LittleEndian.PutUint16(block[5:7], uint16(int16(500)))
	return block
}

func imaBlock(align int) []byte {
	block := make([]byte, align)
	binary.LittleEndian.PutUint16(block[0:2], uint16(int16(1000)))
	return block
}

// adpcmDescriptor states the signal and the codec extension a container
// carries for it, which is everything this family needs to read a block.
func adpcmDescriptor(t *testing.T, signal sample.Signal, variant adpcm.Variant, samples int, schema mediaschema.Descriptor) stream.Descriptor {
	t.Helper()
	extension := make([]byte, 2)
	binary.LittleEndian.PutUint16(extension, uint16(samples))
	if variant == adpcm.Microsoft {
		extension = append(extension, make([]byte, 2+7*4)...)
		binary.LittleEndian.PutUint16(extension[2:4], 7)
		for index, pair := range [][2]int16{{256, 0}, {512, -256}, {0, 0}, {192, 64}, {240, 0}, {460, -208}, {392, -232}} {
			binary.LittleEndian.PutUint16(extension[4+index*4:], uint16(pair[0]))
			binary.LittleEndian.PutUint16(extension[6+index*4:], uint16(pair[1]))
		}
	}
	properties, err := signal.Properties()
	if err != nil {
		t.Fatal(err)
	}
	if properties, err = codec.WithParameters(properties, codec.NewParameters(extension)); err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor("coded", schema, timing.MustBase(1, int64(signal.Rate)), properties)
}
