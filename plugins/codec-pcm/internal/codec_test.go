package internal

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoderEncoderRoundtrip(t *testing.T) {
	dec := NewDecoder(DefaultDecoderConfig)
	enc := NewEncoder(DefaultEncoderConfig)

	in := []byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0xBB, 0xCC, 0xDD}
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

func TestDecoderEncoder24BitRoundtrip(t *testing.T) {
	cfg := DefaultDecoderConfig
	cfg.Format = media.SampleFormatS24
	dec := NewDecoder(cfg)
	enc := NewEncoder(DefaultEncoderConfig)

	// 2 channels * 3 bytes/sample * 3 samples = 18 bytes
	in := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
		0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C,
		0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12,
	}
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

	af, ok := (*frame).(*media.AudioFrame)
	if !ok {
		t.Fatalf("expected audio frame")
	}
	if af.Format != media.SampleFormatS24 {
		t.Fatalf("expected Format S24, got %v", af.Format)
	}
	if af.Samples != 3 {
		t.Fatalf("expected 3 samples, got %v", af.Samples)
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
			cfg := DefaultDecoderConfig
			cfg.CodecID = tt.codec
			dec := NewDecoder(cfg)
			cfgEnc := DefaultEncoderConfig
			cfgEnc.CodecID = tt.codec
			enc := NewEncoder(cfgEnc)

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
	dec := NewDecoder(DefaultDecoderConfig)
	enc := NewEncoder(DefaultEncoderConfig)

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

func TestG711Endianness(t *testing.T) {
	in := []byte{0x00, 0x55, 0xAA, 0xFF}

	// Decode with Little Endian
	cfgLE := DefaultDecoderConfig
	cfgLE.CodecID = media.CodecPCMU
	cfgLE.ByteOrder = binary.LittleEndian
	decLE := NewDecoder(cfgLE)
	pktLE := media.NewPacket(len(in), media.WithPts(100))
	copy(pktLE.Data(), in)
	pktLE.MediaType = media.MediaAudio
	_ = decLE.SendPacket(pktLE)
	frameLE, _ := decLE.ReceiveFrame()
	dataLE := (*frameLE).(*media.AudioFrame).Planes()[0]

	// Decode with Big Endian
	cfgBE := DefaultDecoderConfig
	cfgBE.CodecID = media.CodecPCMU
	cfgBE.ByteOrder = binary.BigEndian
	decBE := NewDecoder(cfgBE)
	pktBE := media.NewPacket(len(in), media.WithPts(100))
	copy(pktBE.Data(), in)
	pktBE.MediaType = media.MediaAudio
	_ = decBE.SendPacket(pktBE)
	frameBE, _ := decBE.ReceiveFrame()
	dataBE := (*frameBE).(*media.AudioFrame).Planes()[0]

	if len(dataLE) != len(dataBE) {
		t.Fatalf("decoded lengths mismatch")
	}

	for i := 0; i < len(dataLE); i += 2 {
		if dataLE[i] != dataBE[i+1] || dataLE[i+1] != dataBE[i] {
			t.Errorf("endianness mismatch at sample %d: LE=[%02x %02x], BE=[%02x %02x]", i/2, dataLE[i], dataLE[i+1], dataBE[i], dataBE[i+1])
		}
	}

	// Now test encoder with BigEndian
	encBE := NewEncoder(EncoderConfig{CodecID: media.CodecPCMU, ByteOrder: binary.BigEndian})
	_ = encBE.SendFrame(frameBE)
	outPktBE, _ := encBE.ReceivePacket()
	if !bytes.Equal(outPktBE.Data(), in) {
		t.Errorf("BigEndian encode mismatch: got %x want %x", outPktBE.Data(), in)
	}
}
