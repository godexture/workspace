package codec

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/media/packet"
)

func TestPacketsSchemaUsesCodecPacketPayload(t *testing.T) {
	typ := Packets()
	if !typ.Valid() || typ.Descriptor().Payload() != reflect.TypeFor[packet.Packet]() {
		t.Fatalf("packet schema = %#v", typ.Descriptor())
	}
	traits := typ.Traits()
	if traits.Fork == nil || traits.Drop == nil || traits.Size == nil || traits.Time == nil {
		t.Fatalf("packet schema traits = %#v", traits)
	}
}
