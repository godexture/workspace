package sample

import (
	"testing"

	"github.com/godexture/godec/media/property"
)

// A companded or compressed stream states a signal and no storage
// representation. Reading the signal has to succeed there, and reading a
// description has to fail, because that is how a consumer that can only handle
// stored scalars declines the stream instead of misreading it.
func TestSignalReadsWithoutAStorageRepresentation(t *testing.T) {
	signal := Signal{Rate: 8_000, Layout: Mono()}
	properties, err := signal.Properties()
	if err != nil {
		t.Fatal(err)
	}
	if properties.Len() != 2 {
		t.Fatalf("properties = %d, want rate and layout only", properties.Len())
	}
	got, err := SignalOf(properties)
	if err != nil || got != signal {
		t.Fatalf("signal = %#v, %v", got, err)
	}
	if _, err := FromProperties(properties); err == nil {
		t.Fatal("a stream with no storage representation was read as a description")
	}
}

// The depth of the signal and the width of the container that holds it are
// different numbers. A companded stream cannot state the first one, so a
// signal without it stays valid rather than forcing its format to invent one.
func TestSignalDepthIsOptional(t *testing.T) {
	unstated := Signal{Rate: 8_000, Layout: Mono()}
	stated := Signal{Rate: 8_000, Layout: Mono(), ValidBits: 14}
	if !unstated.Valid() || !stated.Valid() {
		t.Fatal("a signal was rejected for its depth")
	}
	first, err := unstated.Properties()
	if err != nil {
		t.Fatal(err)
	}
	second, err := stated.Properties()
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("an unstated depth and a stated one share canonical state")
	}
	// Restating a signal without a depth has to remove the one the set carried,
	// so the result never claims a depth this signal does not state.
	cleared, err := unstated.Apply(second)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := validBits.Get(cleared); ok {
		t.Fatalf("valid bits survived a signal that states none: %d", value)
	}
}

func TestInvalidSignalsAreRejected(t *testing.T) {
	for name, value := range map[string]Signal{
		"no rate":          {Layout: Mono()},
		"no layout":        {Rate: 48_000},
		"negative depth":   {Rate: 48_000, Layout: Mono(), ValidBits: -1},
		"impossible depth": {Rate: 48_000, Layout: Mono(), ValidBits: 65},
	} {
		if value.Valid() {
			t.Errorf("%s was accepted", name)
		}
		if _, err := value.Properties(); err == nil {
			t.Errorf("%s produced properties", name)
		}
	}
}

// A description is a signal that also says how its samples are stored, so the
// signal it carries reads back the same either way.
func TestDescriptionCarriesItsSignal(t *testing.T) {
	description := Description{
		Signal:  Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 24},
		Coding:  S24,
		Packing: Interleaved,
		Endian:  LittleEndian,
	}
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	signal, err := SignalOf(properties)
	if err != nil || signal != description.Signal {
		t.Fatalf("signal = %#v, %v", signal, err)
	}
	if description.Rate != 48_000 || description.Layout != Stereo() || description.ValidBits != 24 {
		t.Fatal("description does not promote its signal fields")
	}
	// Decoding changes the storage representation and nothing else.
	if description.Decoded().Signal != description.Signal {
		t.Fatal("decoding changed the signal")
	}
}

func TestSignalApplyPreservesUnknownProperties(t *testing.T) {
	foreign := property.Define[foreignPropertyID](property.Scalar[string]())
	properties, err := property.Put(property.New(), foreign, "kept")
	if err != nil {
		t.Fatal(err)
	}
	properties, err = Signal{Rate: 44_100, Layout: Mono()}.Apply(properties)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := foreign.Get(properties); !ok || value != "kept" {
		t.Fatalf("foreign property = %q, %v", value, ok)
	}
}
