package internal

import (
	"bytes"
	"encoding/hex"
	"io"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
)

const appendixDExample1Hex = "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a"

func TestProbe(t *testing.T) {
	if got := Probe(bytes.NewReader(mustDecodeHex(t, appendixDExample1Hex))); got != manifest.ProbeExactSignature {
		t.Fatalf("Probe() = %d, want %d", got, manifest.ProbeExactSignature)
	}
	if got := Probe(bytes.NewReader([]byte("nope"))); got != manifest.ProbeMismatch {
		t.Fatalf("Probe(non-flac) = %d, want %d", got, manifest.ProbeMismatch)
	}
}

func TestDemuxerAnalyzeAndReadPacket(t *testing.T) {
	data := mustDecodeHex(t, appendixDExample1Hex)
	demuxer, err := NewDemuxer(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, meta, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if meta.AllRaw() != nil {
		t.Fatalf("expected no global raw metadata, got %v", meta.AllRaw())
	}
	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.Codec != media.CodecFLAC {
		t.Fatalf("codec = %s, want %s", stream.Codec, media.CodecFLAC)
	}
	if stream.Audio.SampleRate != 44100 {
		t.Fatalf("sample rate = %d, want 44100", stream.Audio.SampleRate)
	}
	if stream.Audio.ChannelCount() != 2 {
		t.Fatalf("channels = %d, want 2", stream.Audio.ChannelCount())
	}
	if stream.Audio.Format != media.SampleFormatS16 {
		t.Fatalf("format = %s, want %s", stream.Audio.Format, media.SampleFormatS16)
	}
	if raw, ok := stream.Metadata.GetRaw(streamInfoMetadataKey); !ok || len(raw) != 1 || len(raw[0]) != streamInfoLength {
		t.Fatalf("missing STREAMINFO raw metadata: ok=%v len=%d", ok, len(raw))
	}

	pkt, idx, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if idx != 0 || pkt.StreamIndex != 0 {
		t.Fatalf("stream index = (%d, %d), want 0", idx, pkt.StreamIndex)
	}
	if pkt.MediaType != media.MediaAudio {
		t.Fatalf("packet media type = %s, want %s", pkt.MediaType, media.MediaAudio)
	}
	wantFrameBytes := data[42:]
	if !bytes.Equal(pkt.Data(), wantFrameBytes) {
		t.Fatalf("packet data = % x, want % x", pkt.Data(), wantFrameBytes)
	}

	pkt.Release()
	pkt, _, err = demuxer.ReadPacket()
	if err != io.EOF || pkt != nil {
		t.Fatalf("second ReadPacket() = pkt %v err %v, want EOF", pkt, err)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString() error = %v", err)
	}
	return data
}
