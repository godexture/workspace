package test

import (
	"bytes"
	"testing"

	"github.com/godexture/core/domain/media"
	wavpkg "github.com/godexture/format-wav"
)

func TestWAVDemuxerMuxerRoundtrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	muxer := wavpkg.NewMuxerEngine(&buf, wavpkg.MuxerConfig{})

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

	payload := []byte{0x10, 0x00, 0x20, 0x00}
	pkt := media.NewPacket(len(payload))
	copy(pkt.Data(), payload)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	original := buf.Bytes()

	demux, err := wavpkg.NewDemuxerEngine(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("NewDemuxerEngine() error = %v", err)
	}

	streams, _, err := demux.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("Analyze returned %d streams", len(streams))
	}

	pkt2, _, err := demux.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}

	var out bytes.Buffer
	muxer2 := wavpkg.NewMuxerEngine(&out, wavpkg.MuxerConfig{})
	if _, err := muxer2.AddStream(streams[0]); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer2.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer2.WritePacket(0, pkt2); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer2.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	if !bytes.Equal(original, out.Bytes()) {
		t.Fatalf("roundtrip mismatch: original %d bytes vs remuxed %d bytes", len(original), len(out.Bytes()))
	}
}
