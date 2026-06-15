package internal

import (
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
