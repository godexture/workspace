package format

import "testing"

func TestFormatDeclaresAlternativeAndOpenCarriers(t *testing.T) {
	carrier := NewCarrier("wave.data", "format:wave")
	value, err := New(NewTag("fixture", "raw"), []Alternative{AnyOf(SequentialRead), AnyOf(RandomRead, StableSize)}, []Carrier{carrier})
	if err != nil {
		t.Fatal(err)
	}
	if !value.Valid() || len(value.Alternatives()) != 2 || value.Carriers()[0].Owner != "format:wave" {
		t.Fatalf("format = %#v", value)
	}
	if _, err := New("", []Alternative{AnyOf(SequentialRead)}, nil); err == nil {
		t.Fatal("empty format identity accepted")
	}
}

func TestPacketizedFormatMayOmitCapabilityAlternative(t *testing.T) {
	value, err := NewPacketized("fixture:packetized", nil)
	if err != nil || !value.Packetized() {
		t.Fatalf("packetized format = %#v, %v", value, err)
	}
}
