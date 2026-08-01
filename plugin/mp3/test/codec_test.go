package test

import (
	"testing"

	"github.com/godexture/godec/plugin/mp3/internal/codec"
	"github.com/godexture/godec/core/domain/media"
)

func TestDecoder_EmptyPacket(t *testing.T) {
	t.Parallel()
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
