package format

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/carrier"
)

type fixtureFormatID struct{}
type fixturePacketizedFormatID struct{}
type fixtureCarrierID struct{}

func TestFormatDeclaresAlternativeAndOpenCarriers(t *testing.T) {
	declared := carrier.Define[fixtureCarrierID]()
	value, err := Define[fixtureFormatID]([]access.Alternative{access.AnyOf(access.SequentialRead), access.AnyOf(access.RandomRead, access.StableSize)}, []carrier.ID{declared})
	if err != nil {
		t.Fatal(err)
	}
	if !value.Valid() || len(value.Alternatives()) != 2 || value.Carriers()[0] != declared {
		t.Fatalf("format = %#v", value)
	}
	if _, err := Define[struct{}]([]access.Alternative{access.AnyOf(access.SequentialRead)}, nil); err == nil {
		t.Fatal("empty format identity accepted")
	}
}

func TestPacketizedFormatMayOmitCapabilityAlternative(t *testing.T) {
	value, err := DefinePacketized[fixturePacketizedFormatID](nil)
	if err != nil || !value.Packetized() {
		t.Fatalf("packetized format = %#v, %v", value, err)
	}
}
