package test

import (
	"testing"

	"github.com/godexture/codec-mp3/internal"
	"github.com/godexture/codec-mp3/internal/domain"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoder_EmptyPacket(t *testing.T) {
	decoder := internal.NewDecoder()
	packet := media.NewPacket(0)
	err := decoder.SendPacket(packet)
	if err != nil {
		t.Errorf("SendPacket failed: %v", err)
	}

	frame, err := decoder.ReceiveFrame()
	if err == nil || frame != nil {
		t.Errorf("expected error or nil frame for empty packet, got %v", err)
	}
}

func TestEncoder_Stub(t *testing.T) {
	encoder := internal.NewEncoder(domain.EncoderConfig{})
	audioFrame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutStereo2_0, 44100, 1024)
	var f media.Frame = audioFrame
	err := encoder.SendFrame(&f)
	if err != nil {
		t.Errorf("SendFrame failed: %v", err)
	}

	packet, err := encoder.ReceivePacket()
	if err != engine.ErrEAGAIN || packet != nil {
		t.Errorf("expected ErrEAGAIN and nil packet for stub encoder, got %v", err)
	}
}
