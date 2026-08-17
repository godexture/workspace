package codec

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

func TestPacketsSchemaUsesCodecPacketPayload(t *testing.T) {
	typ := Packets()
	if !typ.Valid() || typ.Descriptor().Payload() != reflect.TypeFor[packet.Packet]() {
		t.Fatalf("packet schema = %#v", typ.Descriptor())
	}
	traits := typ.Traits()
	if traits.Fork == nil || traits.Drop == nil || traits.Size == nil || traits.Time == nil || traits.Order == nil {
		t.Fatalf("packet schema traits = %#v", traits)
	}
}

func TestPacketsExposePTSAsTimeAndDTSAsOrder(t *testing.T) {
	value := packet.NewPacket(
		0,
		timing.SomePTS(timing.NewPTS(11)),
		timing.SomeDTS(timing.NewDTS(7)),
		timing.UnknownDuration(),
		buffer.Handle{},
	)
	if got, ok := Packets().Time(value); !ok || got != 11 {
		t.Fatalf("packet time = %d/%v, want PTS 11", got, ok)
	}
	if got, ok := Packets().Order(value); !ok || got != 7 {
		t.Fatalf("packet order = %d/%v, want DTS 7", got, ok)
	}
	unknown := packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), buffer.Handle{})
	if _, ok := Packets().Order(unknown); ok {
		t.Fatal("unknown DTS produced a known order key")
	}
}
