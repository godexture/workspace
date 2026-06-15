package internal

import (
	"bytes"
	"errors"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoder_SendPacketAfterFlush(t *testing.T) {
	dec := NewDecoder()
	if err := dec.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	pkt := media.NewPacket(10)
	if err := dec.SendPacket(pkt); !errors.Is(err, engine.ErrEOF) {
		t.Errorf("expected ErrEOF after flush, got %v", err)
	}
}

func TestDecoder_ReceiveFrameEmptyFlushed(t *testing.T) {
	dec := NewDecoder()
	if err := dec.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	frame, err := dec.ReceiveFrame()
	if !errors.Is(err, engine.ErrEOF) || frame != nil {
		t.Errorf("expected ErrEOF and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_ReceiveFrameEmptyActive(t *testing.T) {
	dec := NewDecoder()
	frame, err := dec.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || frame != nil {
		t.Errorf("expected ErrEAGAIN and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_ID3SkipSplitted(t *testing.T) {
	dec := NewDecoder()

	// ID3v2 header: "ID3" + version (2 bytes) + flags (1 byte) + size (4 bytes, synchsafe integer: 0x00 0x00 0x00 0x0C = 12 bytes)
	// Total ID3 tag size = 10 (header) + 12 (payload) = 22 bytes.
	id3Header := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0C}
	
	// Send first part of ID3 tag (5 bytes)
	pkt1 := media.NewPacket(5)
	copy(pkt1.Data(), id3Header[:5])
	if err := dec.SendPacket(pkt1); err != nil {
		t.Fatalf("SendPacket 1 failed: %v", err)
	}

	// Try to receive. Should return ErrEAGAIN since we don't have 10 bytes yet to know the ID3 size.
	_, err := dec.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) {
		t.Errorf("expected ErrEAGAIN, got %v", err)
	}
	if dec.id3ToSkip != -1 {
		t.Errorf("expected id3ToSkip to be -1 (undetermined), got %d", dec.id3ToSkip)
	}

	// Send next part of ID3 header + some payload (total 15 bytes in buffer now)
	pkt2 := media.NewPacket(10)
	copy(pkt2.Data(), append(id3Header[5:], []byte{1, 2, 3, 4, 5}...))
	if err := dec.SendPacket(pkt2); err != nil {
		t.Fatalf("SendPacket 2 failed: %v", err)
	}

	// Receive. Should determine ID3 size is 22. It consumes 15 bytes, leaving 7 bytes to skip.
	_, err = dec.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) {
		t.Errorf("expected ErrEAGAIN, got %v", err)
	}
	if dec.id3ToSkip != 7 {
		t.Errorf("expected id3ToSkip to be 7, got %d", dec.id3ToSkip)
	}
	if dec.buf.Len() != 0 {
		t.Errorf("expected buffer to be empty, got %d bytes", dec.buf.Len())
	}

	// Send remaining 7 bytes of ID3 + a dummy byte (total 8 bytes)
	pkt3 := media.NewPacket(8)
	copy(pkt3.Data(), []byte{6, 7, 8, 9, 10, 11, 12, 0xFF}) // 0xFF is start of MP3 sync word but not a full frame
	if err := dec.SendPacket(pkt3); err != nil {
		t.Fatalf("SendPacket 3 failed: %v", err)
	}

	// Receive. Should skip remaining 7 bytes of ID3, leaving 0xFF in buffer.
	// Since 0xFF is not a full frame, it should return ErrEAGAIN.
	_, err = dec.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) {
		t.Errorf("expected ErrEAGAIN, got %v", err)
	}
	if dec.id3ToSkip != 0 {
		t.Errorf("expected id3ToSkip to be 0, got %d", dec.id3ToSkip)
	}
	if !bytes.Equal(dec.buf.Bytes(), []byte{0xFF}) {
		t.Errorf("expected buffer to have [0xFF], got %v", dec.buf.Bytes())
	}
}

func TestDecoder_ChannelLayoutChange(t *testing.T) {
	dec := NewDecoder()
	dec.sampleRate = 44100
	dec.channels = 2

	// Mock info indicating a layout change (e.g. mono: 1 channel)
	info := mp3.Mp3DecFrameInfo{
		FrameBytes:  10,
		FrameOffset: 0,
		Channels:    1,
		Hz:          44100,
	}

	floatPcm := make([]float32, 1152)
	intPcm := make([]int16, 1152)
	frame, err := processFrame(floatPcm, intPcm, 576, info)
	if err != nil {
		t.Fatalf("processFrame failed: %v", err)
	}

	audioFrame, ok := frame.(*media.AudioFrame)
	if !ok {
		t.Fatalf("expected *media.AudioFrame")
	}

	if audioFrame.Layout != media.LayoutMono1 {
		t.Errorf("expected LayoutMono1, got %v", audioFrame.Layout)
	}
}
