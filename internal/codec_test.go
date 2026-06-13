package internal_test

import (
	"testing"

	"github.com/godexture/codec-mp3/internal"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

func TestDecoder_EmptyPacket(t *testing.T) {
	dec := internal.NewDecoder()
	pkt := media.NewPacket(0)
	err := dec.SendPacket(pkt)
	if err != nil {
		t.Errorf("SendPacket failed: %v", err)
	}

	frame, err := dec.ReceiveFrame()
	if err == nil || frame != nil {
		t.Errorf("expected error or nil frame for empty packet, got %v", err)
	}
}

func TestEncoder_Stub(t *testing.T) {
	enc := internal.NewEncoder(internal.EncoderConfig{})
	frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutStereo2_0, 44100, 1024)
	var f media.Frame = frame
	err := enc.SendFrame(&f)
	if err != nil {
		t.Errorf("SendFrame failed: %v", err)
	}

	pkt, err := enc.ReceivePacket()
	if err != engine.ErrEAGAIN || pkt != nil {
		t.Errorf("expected ErrEAGAIN and nil packet for stub encoder, got %v", err)
	}
}
