package internal

import (
	"bytes"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoderEncoderRoundtrip(t *testing.T) {
	dec := NewDecoder(DefaultConfig())
	enc := NewEncoder(EncoderConfig{})

	in := []byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0xBB}
	pkt := media.NewPacket(len(in), media.WithPts(42), media.WithDts(42), media.WithStreamIndex(0))
	copy(pkt.Data(), in)
	pkt.MediaType = media.MediaAudio

	if err := dec.SendPacket(pkt); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}

	frame, err := dec.ReceiveFrame()
	if err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}

	if err := enc.SendFrame(frame); err != nil {
		t.Fatalf("SendFrame() error = %v", err)
	}

	out, err := enc.ReceivePacket()
	if err != nil {
		t.Fatalf("ReceivePacket() error = %v", err)
	}

	if !bytes.Equal(out.Data(), in) {
		t.Fatalf("packet data mismatch: got %v want %v", out.Data(), in)
	}
	if out.PTS != 42 {
		t.Fatalf("PTS mismatch: got %d want 42", out.PTS)
	}
}

func TestG711Roundtrip(t *testing.T) {
	tests := []struct {
		name  string
		codec media.CodecID
	}{
		{"PCMU", media.CodecPCMU},
		{"PCMA", media.CodecPCMA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.CodecID = tt.codec
			dec := NewDecoder(cfg)
			enc := NewEncoder(EncoderConfig{CodecID: tt.codec})

			in := []byte{0x00, 0x55, 0xAA, 0xFF, 0x12, 0x34}
			pkt := media.NewPacket(len(in), media.WithPts(100))
			copy(pkt.Data(), in)
			pkt.MediaType = media.MediaAudio

			if err := dec.SendPacket(pkt); err != nil {
				t.Fatalf("SendPacket() error = %v", err)
			}

			frame, err := dec.ReceiveFrame()
			if err != nil {
				t.Fatalf("ReceiveFrame() error = %v", err)
			}

			if err := enc.SendFrame(frame); err != nil {
				t.Fatalf("SendFrame() error = %v", err)
			}

			out, err := enc.ReceivePacket()
			if err != nil {
				t.Fatalf("ReceivePacket() error = %v", err)
			}

			// Note: G.711 is lossy, but encoding then decoding might be lossy.
			// However, if we start with G.711 data, decode it to PCM, then encode it back to G.711,
			// it should ideally match the original G.711 data because G.711 -> PCM -> G.711 is usually reversible for the same codec.
			if !bytes.Equal(out.Data(), in) {
				t.Errorf("packet data mismatch for %s: got %v want %v", tt.codec, out.Data(), in)
			}
		})
	}
}

func TestDecoderEncoderNeedMoreData(t *testing.T) {
	dec := NewDecoder(DefaultConfig())
	enc := NewEncoder(EncoderConfig{})

	if _, err := dec.ReceiveFrame(); err != engine.ErrEAGAIN {
		t.Fatalf("ReceiveFrame() error = %v, want ErrEAGAIN", err)
	}
	if _, err := enc.ReceivePacket(); err != engine.ErrEAGAIN {
		t.Fatalf("ReceivePacket() error = %v, want ErrEAGAIN", err)
	}

	if err := dec.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	if _, err := dec.ReceiveFrame(); err != engine.ErrEOF {
		t.Fatalf("ReceiveFrame() after flush error = %v, want ErrEOF", err)
	}
	if _, err := enc.ReceivePacket(); err != engine.ErrEOF {
		t.Fatalf("ReceivePacket() after flush error = %v, want ErrEOF", err)
	}
}
