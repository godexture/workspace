package internal_test

import (
	"bufio"
	"bytes"
	"os"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-mp3/internal"
)

func TestProbe_ValidMP3(t *testing.T) {
	// 0xFF 0xFB = MPEG1 Layer3 の有効な同期ワード
	mp3Data := []byte{0xFF, 0xFB, 0x90, 0x00}
	score := internal.Probe(bytes.NewReader(mp3Data))
	if score < manifest.ProbeSingleSync {
		t.Errorf("expected ProbeSingleSync or higher, got %d", score)
	}
}

func TestProbe_ID3Header(t *testing.T) {
	id3Data := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(id3Data))
	if score < manifest.ProbeSharedMetadata {
		t.Errorf("expected ProbeSharedMetadata or higher, got %d", score)
	}
}

func TestProbe_NotMP3(t *testing.T) {
	otherData := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(otherData))
	if score != manifest.ProbeMismatch {
		t.Errorf("expected ProbeMismatch, got %d", score)
	}
}

func TestSkipID3v2(t *testing.T) {
	tag := append([]byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}, []byte("abc")...)
	mp3Frame := []byte{0xFF, 0xFB, 0x90, 0x00}
	br := bufio.NewReader(bytes.NewReader(append(tag, mp3Frame...)))

	skipped, err := internal.SkipID3v2(br)
	if err != nil {
		t.Fatalf("SkipID3v2 returned error: %v", err)
	}
	if skipped != len(tag) {
		t.Fatalf("skipped = %d, want %d", skipped, len(tag))
	}

	got, err := br.Peek(len(mp3Frame))
	if err != nil {
		t.Fatalf("Peek returned error: %v", err)
	}
	if !bytes.Equal(got, mp3Frame) {
		t.Fatalf("Peek = %v, want %v", got, mp3Frame)
	}
}

func TestDemuxerAnalyze_ParsesID3Metadata(t *testing.T) {
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	titleFrame := append([]byte("TIT2"), []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x03, 'T', 'e', 's', 't'}...)
	artistFrame := append([]byte("TPE1"), []byte{0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x03, 'A', 'r', 't', 'i', 's', 't'}...)
	tagPayload := append(titleFrame, artistFrame...)
	id3Header := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, byte(len(tagPayload))}
	file := append(append(id3Header, tagPayload...), audio...)

	demuxer, err := internal.NewDemuxer(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}

	streams, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Test"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Artist"})
	metadata.AssertBundleValue(t, &streams[0].Metadata, metadata.KeyTitle("Test"))
}

func TestMuxer_WritesID3Metadata(t *testing.T) {
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var out bytes.Buffer
	muxer := internal.NewMuxer(&out)
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMP3,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream returned error: %v", err)
	}

	meta := *metadata.NewBundle()
	meta.Set(metadata.KeyTitle("Written Title"))
	meta.PushBack(metadata.KeyArtist("Written Artist"))
	if err := muxer.SetMetadata(meta); err != nil {
		t.Fatalf("SetMetadata returned error: %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader returned error: %v", err)
	}

	pkt := media.NewPacket(len(audio))
	copy(pkt.Data(), audio)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket returned error: %v", err)
	}

	demuxer, err := internal.NewDemuxer(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Written Title"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Written Artist"})
}

func TestMuxer_ImplicitHeaderWriting(t *testing.T) {
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var out bytes.Buffer
	muxer := internal.NewMuxer(&out)
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMP3,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream returned error: %v", err)
	}

	meta := *metadata.NewBundle()
	meta.Set(metadata.KeyTitle("Implicit Title"))
	meta.PushBack(metadata.KeyArtist("Implicit Artist"))
	if err := muxer.SetMetadata(meta); err != nil {
		t.Fatalf("SetMetadata returned error: %v", err)
	}
	// WriteHeader explicitly omitted to test implicit write during WritePacket

	pkt := media.NewPacket(len(audio))
	copy(pkt.Data(), audio)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket returned error: %v", err)
	}

	demuxer, err := internal.NewDemuxer(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Implicit Title"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Implicit Artist"})
}
