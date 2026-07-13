package internal

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoder_ReceiveFrameEmptyActive(t *testing.T) {
	decoder := NewDecoder(DefaultDecoderConfig())
	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || frame != nil {
		t.Fatalf("expected ErrEAGAIN and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_ReceiveFrameEmptyFlushed(t *testing.T) {
	decoder := NewDecoder(DefaultDecoderConfig())
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEOF) || frame != nil {
		t.Fatalf("expected ErrEOF and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_SendPacketAfterFlush(t *testing.T) {
	decoder := NewDecoder(DefaultDecoderConfig())
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	packet := media.NewPacket(1)
	if err := decoder.SendPacket(packet); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("expected ErrEOF after flush, got %v", err)
	}
}

func TestDecoder_SendNilPacket(t *testing.T) {
	decoder := NewDecoder(DefaultDecoderConfig())
	if err := decoder.SendPacket(nil); err == nil {
		t.Fatal("expected error for nil packet")
	}
}

func TestDecoder_DecodeRawFrameRFC9639AppendixDExample1(t *testing.T) {
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	assertDecodeAppendixDExample1(t, data[42:], DecoderConfig{StreamInfo: data[8:42]})
}

func TestDecoder_DecodeNativeStreamCompatibility(t *testing.T) {
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	assertDecodeAppendixDExample1(t, data, DefaultDecoderConfig())
}

func assertDecodeAppendixDExample1(t *testing.T, data []byte, config DecoderConfig) {
	t.Helper()
	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio

	decoder := NewDecoder(config)
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}

	frame, err := decoder.ReceiveFrame()
	if err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if frame == nil || *frame == nil {
		t.Fatal("expected decoded frame")
	}

	audioFrame, ok := (*frame).(*media.AudioFrame)
	if !ok {
		t.Fatalf("expected *media.AudioFrame, got %T", *frame)
	}
	if audioFrame.Format != media.SampleFormatS16 {
		t.Fatalf("format = %s, want %s", audioFrame.Format, media.SampleFormatS16)
	}
	if audioFrame.Layout.ChannelCount() != 2 {
		t.Fatalf("channels = %d, want 2", audioFrame.Layout.ChannelCount())
	}
	if audioFrame.SampleRate != 44100 {
		t.Fatalf("sample rate = %d, want 44100", audioFrame.SampleRate)
	}
	if audioFrame.Samples != 1 {
		t.Fatalf("samples = %d, want 1", audioFrame.Samples)
	}

	want := []byte{0xf4, 0x63, 0xb0, 0x28} // left=25588, right=10416, little-endian S16
	if got := audioFrame.Planes()[0]; !bytes.Equal(got, want) {
		t.Fatalf("decoded PCM = % x, want % x", got, want)
	}
}

func TestDecoder_PartialStreamNeedsMoreData(t *testing.T) {
	data := []byte("fLaC")
	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio

	decoder := NewDecoder(DefaultDecoderConfig())
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}
	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || frame != nil {
		t.Fatalf("expected ErrEAGAIN and nil frame for partial stream, got err=%v, frame=%v", err, frame)
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
