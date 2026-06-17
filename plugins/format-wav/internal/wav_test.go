package internal

import (
	"bytes"
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestProbeRecognizesWAVSignature(t *testing.T) {
	data := buildTestWAV(t, []byte{0x01, 0x02, 0x03, 0x04})

	if got := Probe(bytes.NewReader(data)); got != 100 {
		t.Fatalf("Probe() = %d, want %d", got, 100)
	}
}

func TestWAVRoundTripMonoPCM16(t *testing.T) {
	original := []byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}
	wavData := buildTestWAV(t, original)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream.Type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.MediaAttributes.Audio.Format != media.SampleFormatS16 {
		t.Fatalf("stream format = %s, want %s", stream.MediaAttributes.Audio.Format, media.SampleFormatS16)
	}
	if stream.MediaAttributes.Audio.SampleRate != 48000 {
		t.Fatalf("stream sample rate = %d, want %d", stream.MediaAttributes.Audio.SampleRate, 48000)
	}
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != 1 {
		t.Fatalf("stream channels = %d, want 1", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount())
	}

	pkt, streamIndex, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if streamIndex != 0 {
		t.Fatalf("streamIndex = %d, want 0", streamIndex)
	}
	if !bytes.Equal(pkt.Data(), original) {
		t.Fatalf("packet data mismatch: got %v, want %v", pkt.Data(), original)
	}

	var out bytes.Buffer
	muxer := NewMuxer(&out)
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	if !bytes.Equal(out.Bytes(), wavData) {
		t.Fatalf("muxed wav mismatch: got %d bytes, want %d bytes", len(out.Bytes()), len(wavData))
	}
}

func buildTestWAV(t *testing.T, payload []byte) []byte {
	t.Helper()

	var out bytes.Buffer
	muxer := NewMuxer(&out)
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    48000,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutMono1,
			},
		},
	}

	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	pkt := media.NewPacket(len(payload))
	copy(pkt.Data(), payload)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	return out.Bytes()
}
