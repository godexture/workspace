package property

import "testing"

type sampleRateID struct{}
type unknownPropertyID struct{}

func TestTypedPropertySetIsImmutableAndOpen(t *testing.T) {
	rate := Define[sampleRateID, int]()
	unknown := Define[unknownPropertyID, []byte]()
	first, err := rate.Set(New(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{1, 2, 3}
	second, err := unknown.Set(first, raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 9
	if got, ok := rate.Get(first); !ok || got != 48000 {
		t.Fatalf("first rate = %d, %v", got, ok)
	}
	if _, ok := unknown.Get(first); ok {
		t.Fatal("mutation leaked into original set")
	}
	got, ok := unknown.Get(second)
	if !ok || got[0] != 1 {
		t.Fatalf("unknown property = %v, %v", got, ok)
	}
	got[0] = 7
	again, _ := unknown.Get(second)
	if again[0] != 1 {
		t.Fatal("property byte value was not copied")
	}
}

func TestPropertyTypeAndUnknownLookup(t *testing.T) {
	rate := Define[sampleRateID, int]()
	if _, err := New().With(rate, "48000"); err == nil {
		t.Fatal("wrong property type accepted")
	}
	set, _ := rate.Set(New(), 44100)
	if _, ok := set.Lookup(IdentityOf[unknownPropertyID]()); ok {
		t.Fatal("unknown property unexpectedly present")
	}
}
