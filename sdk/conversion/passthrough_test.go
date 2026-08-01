package conversion_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/conversion"
	"github.com/godexture/godec/sdk/testutil/audio"

	wav "github.com/godexture/godec/plugin/wave"
)

// TestBuildOmittedCodecStillOpensDecoderAndEncoder is the M0 baseline for
// docs/refactor/checkpoint.md's "target codec/formatを省略した現行routeが
// decoder/encoderを開くことを明示的に検査する". The current implementation
// has no stream-copy path (that is C4/M7 scope): even a same-format,
// same-codec conversion with Spec.Codec left empty must resolve a decoder
// and an encoder and actually decode/re-encode every sample, not detect
// "input already matches" and pass packets through unopened. This test
// exists to be the thing that starts failing the day M7 adds copy/remux,
// which is exactly the point -- it pins today's behavior as a comparison
// baseline, not a requirement to keep.
func TestBuildOmittedCodecStillOpensDecoderAndEncoder(t *testing.T) {
	var wavBuf bytes.Buffer
	writeTestWAV(t, &wavBuf)

	var output bytes.Buffer
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBuf.Bytes())}, &output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		// Codec intentionally left empty: this is the "target format
		// omitted" route the checkpoint asks to pin.
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()

	roles := map[manifest.NodeType]int{}
	for _, n := range built.Description().Nodes {
		roles[n.Role]++
	}
	if roles[manifest.RoleDecoder] == 0 {
		t.Fatalf("omitted-codec route did not open a decoder node; roles = %v", roles)
	}
	if roles[manifest.RoleEncoder] == 0 {
		t.Fatalf("omitted-codec route did not open an encoder node; roles = %v", roles)
	}

	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("Run() produced no output")
	}
}

// TestBuildPreservesKnownMetadataThroughOmittedCodecRoute freezes today's
// metadata propagation for the decode/re-encode route above: a title tag
// set on the source stream survives to the muxed output, because
// core/routing wires the demuxer's parsed metadata.Bundle straight into the
// muxer's SetMetadata. It says nothing about unknown/raw or
// duplicate/ordered metadata in general (docs/refactor/media.md's open
// metadata.Document contract doesn't exist until M3) -- see format-wav's
// own TestWAVMetadataRoundTrip for that lower-level, format-specific
// coverage. Byte-equal output here is not treated as evidence of a stream
// copy: the roles check above already pins that decode/encode still runs.
func TestBuildPreservesKnownMetadataThroughOmittedCodecRoute(t *testing.T) {
	// A seekable source and output are required: the WAVE muxer only
	// writes a fixed, forward-discoverable "data" chunk size (and so only
	// reliably round-trips its trailer LIST/metadata chunk to a plain
	// forward parser) when it can detect a seekable writer. Against a
	// plain, non-seekable io.Writer it emits an unknown-length streaming
	// header instead, and format-wav/internal's own parseHeader stops
	// scanning chunks the moment it sees that sentinel -- a real,
	// independently-pinnable baseline quirk, but not what this test is
	// about, so it is isolated here rather than tested via *bytes.Buffer.
	wavBuf := audio.NewBuffer(nil)
	writeTestWAVWithTitle(t, wavBuf, "Known Title Baseline")

	output := audio.NewBuffer(nil)
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBuf.Bytes())}, output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	demuxer, err := wav.NewDemuxerEngine(bytes.NewReader(output.Bytes()), wav.MustNewDemuxerConfig())
	if err != nil {
		t.Fatalf("NewDemuxerEngine() error = %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Known Title Baseline"))
}

// writeTestWAVWithTitle writes the same short synthetic tone as
// writeTestWAV, but through the real WAVE muxer engine with a title tag
// set, so the source carries known metadata for propagation tests.
func writeTestWAVWithTitle(t *testing.T, buf *audio.Buffer, title string) {
	t.Helper()
	const sampleRate = 8000
	const numSamples = 4000

	muxer, err := wav.NewMuxerEngine(buf, wav.MustNewMuxerConfig())
	if err != nil {
		t.Fatalf("NewMuxerEngine() error = %v", err)
	}
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    sampleRate,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutMono1,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	meta := metadata.NewBundle()
	meta.Set(metadata.KeyTitle(title))
	if err := muxer.SetMetadata(*meta); err != nil {
		t.Fatalf("SetMetadata() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	samples := make([]int16, numSamples)
	for i := range samples {
		samples[i] = int16(float64(i%200-100) * 40)
	}
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		data[2*i] = byte(s)
		data[2*i+1] = byte(s >> 8)
	}
	packet := media.NewPacketFromData(data)
	if err := muxer.WritePacket(0, packet); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}
}

// TestBuildPreservesOrderedMultiValueMetadataThroughOmittedCodecRoute
// extends the single-key baseline above to metadata.KeyArtist, a `multiple`
// key: two IART INFO subchunks in the source must survive the decode/
// re-encode route above as two ordered values, not collapse to one or
// reorder, per docs/refactor/checkpoint.md M0-R3.
func TestBuildPreservesOrderedMultiValueMetadataThroughOmittedCodecRoute(t *testing.T) {
	wavBytes := buildTestWAVWithInfoTags(t, []wavInfoTag{
		{id: "IART", value: "Artist One"},
		{id: "IART", value: "Artist Two"},
	}, nil)

	output := audio.NewBuffer(nil)
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBytes)}, output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	bundle := demuxWAVMetadata(t, output.Bytes())
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Artist One", "Artist Two"})
}

// TestBuildOverwritesDuplicateSingleValueMetadataThroughOmittedCodecRoute
// pins today's behavior for a `single` key (metadata.KeyComment) repeated
// in the source: baseBundle.set unconditionally overwrites by type, so the
// last ICMT subchunk read wins and the first is silently discarded. This is
// exactly the kind of duplicate-handling the checkpoint asks to freeze,
// whether or not "last wins" is the eventual desired policy.
func TestBuildOverwritesDuplicateSingleValueMetadataThroughOmittedCodecRoute(t *testing.T) {
	wavBytes := buildTestWAVWithInfoTags(t, []wavInfoTag{
		{id: "ICMT", value: "First Comment"},
		{id: "ICMT", value: "Second Comment"},
	}, nil)

	output := audio.NewBuffer(nil)
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBytes)}, output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	bundle := demuxWAVMetadata(t, output.Bytes())
	metadata.AssertBundleValue(t, &bundle, metadata.KeyComment("Second Comment"))
}

// TestBuildDropsUnrecognizedMetadataThroughOmittedCodecRoute pins the
// current silent-loss behavior the checkpoint asks to make explicit: a WAV
// INFO subchunk tag with no case in mapWavInfoTag (here "IENG", Engineer)
// has no raw fallback at parse time, so it disappears completely -- not
// merely unmapped to a known key, but absent from the Bundle's raw map too,
// with no warning of any kind. A known title tag alongside it is the
// control proving the rest of metadata handling still worked.
func TestBuildDropsUnrecognizedMetadataThroughOmittedCodecRoute(t *testing.T) {
	wavBytes := buildTestWAVWithInfoTags(t, []wavInfoTag{
		{id: "INAM", value: "Known Title"},
		{id: "IENG", value: "Should Be Lost"},
	}, nil)

	output := audio.NewBuffer(nil)
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBytes)}, output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	bundle := demuxWAVMetadata(t, output.Bytes())
	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Known Title"))
	if raw := bundle.AllRaw(); len(raw) != 0 {
		t.Fatalf("unrecognized INFO tag left a trace in raw metadata: %v (loss is expected to be total, not partial)", raw)
	}
}

// TestBuildPreservesRawCueChunkThroughOmittedCodecRoute is the
// conversion.Build()-route counterpart to format-wav's own lower-level
// TestWAVMetadataRoundTrip: an opaque "cue " chunk (preserved via
// metadata.Bundle's raw map, since Godec has no typed cue-point model) must
// still come out byte-identical after going through the full decode/
// re-encode route this package tests, not just a direct demux/mux
// round-trip.
func TestBuildPreservesRawCueChunkThroughOmittedCodecRoute(t *testing.T) {
	cuePayload := []byte{0x01, 0x00, 0x00, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}
	wavBytes := buildTestWAVWithInfoTags(t, nil, cuePayload)

	output := audio.NewBuffer(nil)
	built, err := conversion.Build(context.Background(), conversion.InputSet{Main: bytes.NewReader(wavBytes)}, output, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	}, pipeline.ObservationOff)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	defer built.Close()
	if err := built.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	bundle := demuxWAVMetadata(t, output.Bytes())
	raw, exists := bundle.GetRaw("cue ")
	if !exists || len(raw) == 0 || !bytes.Equal(raw[0], cuePayload) {
		t.Fatalf("raw cue chunk = %v (exists=%v), want %v", raw, exists, cuePayload)
	}
}

// wavInfoTag is one LIST/INFO subchunk (id, value) pair for
// buildTestWAVWithInfoTags, in the order it should appear in the file.
type wavInfoTag struct {
	id    string
	value string
}

// buildTestWAVWithInfoTags hand-assembles a minimal RIFF/WAVE byte stream
// (fmt chunk + a short PCM data chunk) with a LIST/INFO chunk carrying the
// given subchunks in order, and an optional raw "cue " chunk -- unlike
// writeTestWAVWithTitle, this does not go through wav.NewMuxerEngine, whose
// metadata writer only ever emits one subchunk per known key from a typed
// Bundle and so cannot produce the duplicate/unrecognized-tag inputs these
// tests need to drive the demuxer with.
func buildTestWAVWithInfoTags(t *testing.T, tags []wavInfoTag, cuePayload []byte) []byte {
	t.Helper()
	const sampleRate = 8000
	const numSamples = 4000

	samples := make([]int16, numSamples)
	for i := range samples {
		samples[i] = int16(float64(i%200-100) * 40)
	}
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		data[2*i] = byte(s)
		data[2*i+1] = byte(s >> 8)
	}

	var chunks bytes.Buffer
	chunks.WriteString("fmt ")
	_ = binary.Write(&chunks, binary.LittleEndian, uint32(16))
	_ = binary.Write(&chunks, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&chunks, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&chunks, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&chunks, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	_ = binary.Write(&chunks, binary.LittleEndian, uint16(2))            // block align
	_ = binary.Write(&chunks, binary.LittleEndian, uint16(16))           // bits per sample

	if len(tags) > 0 {
		var info bytes.Buffer
		info.WriteString("INFO")
		for _, tag := range tags {
			info.Write(wavRIFFChunk(tag.id, append([]byte(tag.value), 0)))
		}
		chunks.Write(wavRIFFChunk("LIST", info.Bytes()))
	}

	if cuePayload != nil {
		chunks.Write(wavRIFFChunk("cue ", cuePayload))
	}

	chunks.Write(wavRIFFChunk("data", data))

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(4+chunks.Len()))
	out.WriteString("WAVE")
	out.Write(chunks.Bytes())
	return out.Bytes()
}

// wavRIFFChunk builds one RIFF chunk (4-byte id, little-endian uint32 size,
// payload, zero pad byte if the payload length is odd), matching the
// layout plugin/wave/internal's parser expects.
func wavRIFFChunk(id string, payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(id)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	if len(payload)%2 == 1 {
		buf.WriteByte(0)
	}
	return buf.Bytes()
}

// demuxWAVMetadata parses wavBytes with the WAVE demuxer and returns its
// metadata.Bundle, failing the test on any error.
func demuxWAVMetadata(t *testing.T, wavBytes []byte) metadata.Bundle {
	t.Helper()
	demuxer, err := wav.NewDemuxerEngine(bytes.NewReader(wavBytes), wav.MustNewDemuxerConfig())
	if err != nil {
		t.Fatalf("NewDemuxerEngine() error = %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return bundle
}
