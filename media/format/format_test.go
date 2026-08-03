package format

import "testing"

type fixtureFormatID struct{}
type fixturePacketizedFormatID struct{}

func TestFormatDeclaresAlternativeAndOpenCarriers(t *testing.T) {
	carrier := NewCarrier("wave.data", "format:wave")
	value, err := Define[fixtureFormatID]([]Alternative{AnyOf(SequentialRead), AnyOf(RandomRead, StableSize)}, []Carrier{carrier})
	if err != nil {
		t.Fatal(err)
	}
	if !value.Valid() || len(value.Alternatives()) != 2 || value.Carriers()[0].Owner != "format:wave" {
		t.Fatalf("format = %#v", value)
	}
	if _, err := Define[struct{}]([]Alternative{AnyOf(SequentialRead)}, nil); err == nil {
		t.Fatal("empty format identity accepted")
	}
}

func TestPacketizedFormatMayOmitCapabilityAlternative(t *testing.T) {
	value, err := DefinePacketized[fixturePacketizedFormatID](nil)
	if err != nil || !value.Packetized() {
		t.Fatalf("packetized format = %#v, %v", value, err)
	}
}
