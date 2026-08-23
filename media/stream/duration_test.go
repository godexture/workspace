package stream

import (
	"testing"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/timing"
)

// A stream that states how long it lasts and one that cannot are different
// facts, and a reader has to be able to tell them apart without treating a
// zero as either.
func TestALengthIsStatedOrAbsentRatherThanZero(t *testing.T) {
	if _, stated := DurationOf(property.New()); stated {
		t.Fatal("an empty property set stated a length")
	}
	properties, err := WithDuration(property.New(), timing.NewDuration(0))
	if err != nil {
		t.Fatal(err)
	}
	value, stated := DurationOf(properties)
	if !stated {
		t.Fatal("a stated length of zero read as absent")
	}
	if got, ok := value.Get(); !ok || got.Int64() != 0 {
		t.Fatalf("length = %v, %v", got, ok)
	}
}

func TestALengthRoundTripsAndCanBeWithdrawn(t *testing.T) {
	properties, err := WithDuration(property.New(), timing.NewDuration(48_000))
	if err != nil {
		t.Fatal(err)
	}
	value, stated := DurationOf(properties)
	if !stated || value.Value().Int64() != 48_000 {
		t.Fatalf("length = %v, %v", value, stated)
	}
	// A stage that stops being able to say how long the stream is withdraws
	// the length rather than leaving one that has stopped being true.
	if _, stated := DurationOf(WithoutDuration(properties)); stated {
		t.Fatal("a withdrawn length is still stated")
	}
}

func TestANegativeLengthIsRefused(t *testing.T) {
	if _, err := WithDuration(property.New(), timing.NewDuration(-1)); err == nil {
		t.Fatal("a negative length was accepted")
	}
}

// The length is part of what a descriptor is, so two streams that differ only
// in how long they last are not the same state.
func TestALengthChangesTheDescriptorFingerprint(t *testing.T) {
	base := property.New()
	first, err := WithDuration(base, timing.NewDuration(1))
	if err != nil {
		t.Fatal(err)
	}
	second, err := WithDuration(base, timing.NewDuration(2))
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("two lengths produced one fingerprint")
	}
}
