package sample

import "testing"

func TestLayoutRoundTripsAChannelMask(t *testing.T) {
	cases := []struct {
		layout Layout
		mask   uint32
		count  int
		text   string
	}{
		{Mono(), 0x4, 1, "FC"},
		{Stereo(), 0x3, 2, "FL+FR"},
		{Positions(FrontLeft, FrontRight, FrontCenter, LowFrequency, BackLeft, BackRight), 0x3f, 6, "FL+FR+FC+LFE+BL+BR"},
	}
	for _, value := range cases {
		if got := value.layout.Mask(); got != value.mask {
			t.Errorf("%s mask = %#x, want %#x", value.text, got, value.mask)
		}
		if got := value.layout.Count(); got != value.count {
			t.Errorf("%s count = %d, want %d", value.text, got, value.count)
		}
		if got := value.layout.String(); got != value.text {
			t.Errorf("layout string = %q, want %q", got, value.text)
		}
		restored, ok := FromMask(value.mask, value.count)
		if !ok || restored != value.layout {
			t.Errorf("%s did not survive its mask: %v, %v", value.text, restored, ok)
		}
	}
}

// A source that stores channels without naming their positions stays
// representable. The layout is not a closed enum of named speaker sets.
func TestLayoutKeepsUnknownPositions(t *testing.T) {
	layout := Channels(3)
	if !layout.Valid() || layout.Positioned() || layout.Count() != 3 || layout.Mask() != 0 {
		t.Fatalf("unpositioned layout = %#v", layout)
	}
	if layout.String() != "3ch" {
		t.Fatalf("layout string = %q", layout.String())
	}
	if _, ok := layout.At(0); ok {
		t.Fatal("unpositioned layout reported a position")
	}
	restored, ok := FromMask(0, 3)
	if !ok || restored != layout {
		t.Fatalf("zero mask = %#v, %v", restored, ok)
	}
	if Channels(3) == Stereo() || Channels(2) == Stereo() {
		t.Fatal("an unpositioned layout compared equal to a positioned one")
	}
}

func TestLayoutOrdersPositionsByMaskBit(t *testing.T) {
	layout := Positions(BackRight, FrontLeft, LowFrequency)
	want := []Position{FrontLeft, LowFrequency, BackRight}
	for index, expected := range want {
		got, ok := layout.At(index)
		if !ok || got != expected {
			t.Fatalf("channel %d = %v, %v, want %v", index, got, ok, expected)
		}
	}
	if _, ok := layout.At(3); ok {
		t.Fatal("layout reported a fourth channel")
	}
	if !layout.Has(LowFrequency) || layout.Has(FrontCenter) || layout.Has(positionCount) {
		t.Fatal("channel membership is wrong")
	}
}

func TestInconsistentLayoutsAreRejected(t *testing.T) {
	if Positions(FrontLeft, FrontLeft).Valid() {
		t.Error("a repeated position was accepted")
	}
	if Positions(positionCount).Valid() || Positions().Valid() {
		t.Error("an unknown or empty position set was accepted")
	}
	if Channels(0).Valid() || Channels(MaxChannels+1).Valid() {
		t.Error("an out-of-range channel count was accepted")
	}
	if _, ok := FromMask(0x7, 2); ok {
		t.Error("a mask naming more channels than the header was accepted")
	}
	if _, ok := FromMask(1<<positionCount, 1); ok {
		t.Error("a mask with reserved bits was accepted")
	}
}

func TestParseLayoutInvertsString(t *testing.T) {
	for _, value := range []Layout{Mono(), Stereo(), Channels(1), Channels(8), Positions(FrontLeft, FrontRight, TopBackRight)} {
		got, ok := ParseLayout(value.String())
		if !ok || got != value {
			t.Errorf("%s round trip = %#v, %v", value, got, ok)
		}
	}
	if got, ok := ParseLayout("fl+fr"); !ok || got != Stereo() {
		t.Errorf("lowercase positions = %#v, %v", got, ok)
	}
	for _, text := range []string{"", "0ch", "65ch", "ch", "FL+FL", "FL+XX", "-1ch"} {
		if _, ok := ParseLayout(text); ok {
			t.Errorf("%q was accepted", text)
		}
	}
}
