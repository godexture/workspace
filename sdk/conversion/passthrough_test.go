package conversion_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/conversion"
	"github.com/godexture/godec/sdk/testutil/audio"

	wav "github.com/godexture/godec/plugins/format-wav"
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
