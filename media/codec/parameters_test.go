package codec

import (
	"testing"

	"github.com/godexture/godec/media/property"
)

// Codec parameters are what a container carries without reading. The contract
// is that they come back unchanged and that changing them changes the stream,
// because a decoder configured differently produces something different.
func TestParametersRoundTripAndReachCanonicalState(t *testing.T) {
	value := NewParameters([]byte{0x07, 0x00, 0x00, 0x01, 0x00, 0x00})
	properties, err := WithParameters(property.New(), value)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParametersOf(properties)
	if !ok || !got.Equal(value) || got.Len() != value.Len() {
		t.Fatalf("parameters = %x, %v", got.AppendTo(nil), ok)
	}

	other, err := WithParameters(property.New(), NewParameters([]byte{0x07, 0x00, 0x00, 0x02, 0x00, 0x00}))
	if err != nil {
		t.Fatal(err)
	}
	if properties.Fingerprint() == other.Fingerprint() {
		t.Fatal("different codec parameters share canonical state")
	}
	if dropped := WithoutParameters(properties); dropped.Len() != 0 {
		t.Fatalf("parameters survived being dropped: %d", dropped.Len())
	}
}

// The payload is copied on the way in and never handed out as a slice a caller
// can write through, which is what makes sharing the backing safe.
func TestParametersDoNotShareMutableStorage(t *testing.T) {
	source := []byte{1, 2, 3}
	value := NewParameters(source)
	source[0] = 9
	if got := value.AppendTo(nil); got[0] != 1 {
		t.Fatalf("parameters followed their source: %x", got)
	}
	handed := value.AppendTo(nil)
	handed[1] = 9
	if got := value.AppendTo(nil); got[1] != 2 {
		t.Fatalf("parameters followed a copy handed out: %x", got)
	}
}

func TestEmptyParametersAreAbsentRatherThanPresent(t *testing.T) {
	if NewParameters(nil).Valid() || NewParameters([]byte{}).Valid() {
		t.Fatal("empty parameters were treated as stated")
	}
	if _, err := WithParameters(property.New(), Parameters{}); err == nil {
		t.Fatal("a stream stated parameters it does not have")
	}
	if _, ok := ParametersOf(property.New()); ok {
		t.Fatal("a stream with no parameters reported some")
	}
}
