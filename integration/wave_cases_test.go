package integration_test

import (
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/testkit"
)

func runWAVECases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	payload := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	encoded := riffWAVE(payload, 2, 48_000, 16)
	truncated := append([]byte(nil), encoded[:len(encoded)-2]...)
	malformed := append([]byte(nil), encoded...)
	copy(malformed[0:4], "NOPE")
	partial := riffWAVE([]byte{1, 2, 3}, 2, 48_000, 16)
	narrowPayload := []byte{0xf0, 0xff, 0x10, 0x00}
	narrow := riffWAVE(narrowPayload, 1, 32_000, 12)

	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, wave.DemuxerIdentity(), "bytes", access.Bytes(), "chunks", mediaformat.Chunks()), coverage),
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:  "riff-pcm-split-inside-frame",
			Input: testkit.ByteInput(encoded[:45], encoded[45:]),
			Want: testkit.WantChunks(
				testkit.Chunk{Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), Bytes: payload[:4]},
				testkit.Chunk{Sequence: 1, PTS: timing.SomePTS(timing.NewPTS(1)), Bytes: payload[4:]},
			),
		},
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:  "extensible-twelve-bit-pcm",
			Input: testkit.ByteInput(narrow),
			Want: testkit.WantChunks(testkit.Chunk{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), Bytes: narrowPayload,
			}),
		},
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:  "malformed-signature",
			Input: testkit.ByteInput(malformed),
			Want:  testkit.WantPlanError[packet.Chunk](wave.ErrMalformed),
		},
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:  "truncated-data",
			Input: testkit.ByteInput(truncated),
			Want:  testkit.WantRunError[packet.Chunk](wave.ErrTruncatedData),
		},
		testkit.Case[buffer.Handle, packet.Chunk]{
			Name:  "partial-sample-block",
			Input: testkit.ByteInput(partial),
			Want:  testkit.WantPlanError[packet.Chunk](wave.ErrMalformed),
		},
	)

	description := sample.Description{
		Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000,
		Layout: sample.Stereo, Endian: sample.LittleEndian,
	}
	testkit.Format(t,
		testkit.Track(testkit.SubjectIn(set, wave.MuxerIdentity(), "packets", codec.Packets(), "writes", access.Writes()), coverage),
		testkit.Case[packet.Packet, access.Write]{
			Name:   "byte-exact-riff",
			Config: config.NewPatch(),
			Input: testkit.PacketInput(description, []testkit.Packet{{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: payload,
			}}),
			Want: testkit.WantWriteImage(muxedWAVE(payload, 2, 48_000, 16)),
		},
		testkit.Case[packet.Packet, access.Write]{
			Name:   "byte-exact-extensible-twelve-bit",
			Config: config.NewPatch(),
			Input: testkit.PacketInput(sample.Description{
				Format: sample.S16Interleaved, ValidBits: 12, Rate: 32_000,
				Layout: sample.Mono, Endian: sample.LittleEndian,
			}, []testkit.Packet{{
				Sequence: 0, PTS: timing.SomePTS(timing.NewPTS(0)), DTS: timing.UnknownDTS(), Duration: timing.SomeDuration(timing.NewDuration(2)), Bytes: narrowPayload,
			}}),
			Want: testkit.WantWriteImage(muxedWAVE(narrowPayload, 1, 32_000, 12)),
		},
	)
}

func riffWAVE(payload []byte, channels uint16, rate uint32, validBits uint16) []byte {
	format := pcmFormat(channels, rate, validBits)
	bodySize := 4 + 8 + len(format) + 8 + len(payload) + len(payload)&1
	result := make([]byte, 8+bodySize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(bodySize))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], uint32(len(format)))
	copy(result[20:], format)
	offset := 20 + len(format)
	copy(result[offset:offset+4], "data")
	binary.LittleEndian.PutUint32(result[offset+4:offset+8], uint32(len(payload)))
	copy(result[offset+8:], payload)
	return result
}

func muxedWAVE(payload []byte, channels uint16, rate uint32, validBits uint16) []byte {
	format := pcmFormat(channels, rate, validBits)
	result := make([]byte, 12+36+8+len(format)+8+len(payload)+len(payload)&1)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "JUNK")
	binary.LittleEndian.PutUint32(result[16:20], 28)
	offset := 48
	copy(result[offset:offset+4], "fmt ")
	binary.LittleEndian.PutUint32(result[offset+4:offset+8], uint32(len(format)))
	copy(result[offset+8:], format)
	offset += 8 + len(format)
	copy(result[offset:offset+4], "data")
	binary.LittleEndian.PutUint32(result[offset+4:offset+8], uint32(len(payload)))
	copy(result[offset+8:], payload)
	return result
}

func pcmFormat(channels uint16, rate uint32, validBits uint16) []byte {
	blockAlign := channels * 2
	if validBits == 16 {
		result := make([]byte, 16)
		binary.LittleEndian.PutUint16(result[0:2], 1)
		binary.LittleEndian.PutUint16(result[2:4], channels)
		binary.LittleEndian.PutUint32(result[4:8], rate)
		binary.LittleEndian.PutUint32(result[8:12], rate*uint32(blockAlign))
		binary.LittleEndian.PutUint16(result[12:14], blockAlign)
		binary.LittleEndian.PutUint16(result[14:16], 16)
		return result
	}
	result := make([]byte, 40)
	binary.LittleEndian.PutUint16(result[0:2], 0xfffe)
	binary.LittleEndian.PutUint16(result[2:4], channels)
	binary.LittleEndian.PutUint32(result[4:8], rate)
	binary.LittleEndian.PutUint32(result[8:12], rate*uint32(blockAlign))
	binary.LittleEndian.PutUint16(result[12:14], blockAlign)
	binary.LittleEndian.PutUint16(result[14:16], 16)
	binary.LittleEndian.PutUint16(result[16:18], 22)
	binary.LittleEndian.PutUint16(result[18:20], validBits)
	if channels == 1 {
		binary.LittleEndian.PutUint32(result[20:24], 4)
	} else {
		binary.LittleEndian.PutUint32(result[20:24], 3)
	}
	binary.LittleEndian.PutUint16(result[24:26], 1)
	copy(result[28:40], []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71})
	return result
}
