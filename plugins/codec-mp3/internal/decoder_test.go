package internal

import (
	"errors"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3/domain"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoder_SendPacketAfterFlush(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder()
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	packet := media.NewPacket(10)
	if err := decoder.SendPacket(packet); !errors.Is(err, engine.ErrEOF) {
		t.Errorf("expected ErrEOF after flush, got %v", err)
	}
}

func TestDecoder_ReceiveFrameEmptyFlushed(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder()
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	audioFrame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEOF) || audioFrame != nil {
		t.Errorf("expected ErrEOF and nil frame, got err=%v, frame=%v", err, audioFrame)
	}
}

func TestDecoder_ReceiveFrameEmptyActive(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder()
	audioFrame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || audioFrame != nil {
		t.Errorf("expected ErrEAGAIN and nil frame, got err=%v, frame=%v", err, audioFrame)
	}
}

func TestDecoder_ChannelLayoutChange(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder()
	decoder.sampleRate = 44100
	decoder.channelCount = 2

	// Mock info indicating a layout change (e.g. mono: 1 channel)
	frameInfo := domain.FrameInfo{
		FrameBytes:      10,
		FrameOffset:     0,
		Channels:        1,
		SampleRateHertz: 44100,
	}

	float32PCMSamples := make([]float32, 1152)
	audioFrame, err := processFrame(float32PCMSamples, 576, frameInfo)
	if err != nil {
		t.Fatalf("processFrame failed: %v", err)
	}

	actualAudioFrame, isAudioFrame := audioFrame.(*media.AudioFrame)
	if !isAudioFrame {
		t.Fatalf("expected *media.AudioFrame")
	}

	if actualAudioFrame.Layout != media.LayoutMono1 {
		t.Errorf("expected LayoutMono1, got %v", actualAudioFrame.Layout)
	}
}
