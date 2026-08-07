package property

import (
	"errors"
	"math"
	"testing"
)

type sampleRateID struct{}
type unknownPropertyID struct{}

func TestTypedPropertySetIsImmutableAndOpen(t *testing.T) {
	rate := Define[sampleRateID](Scalar[int]())
	unknown := Define[unknownPropertyID](func(value []byte) ([]byte, error) {
		return append([]byte("bytes:"), value...), nil
	}, func(value []byte) []byte {
		return append([]byte(nil), value...)
	})
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

func TestReferencePropertyRequiresDeclaredClone(t *testing.T) {
	type pointerID struct{}
	encoder := func(value []int) ([]byte, error) { return []byte("slice"), nil }
	missing := Define[pointerID](encoder)
	if missing.Valid() {
		t.Fatal("reference property without clone was accepted")
	}
	if missing.Problem() == nil {
		t.Fatal("reference property problem was discarded")
	}
	key := Define[pointerID](encoder, func(value []int) []int {
		return append([]int(nil), value...)
	})
	if !key.Valid() {
		t.Fatal("reference property with clone was rejected")
	}
}

func TestInterfacePropertyUsesItsDeclaredClone(t *testing.T) {
	type interfaceID struct{}
	key := Define[interfaceID](func(value any) ([]byte, error) {
		switch typed := value.(type) {
		case []byte:
			return append([]byte("bytes:"), typed...), nil
		default:
			return nil, errors.New("unsupported property value")
		}
	}, func(value any) any {
		switch typed := value.(type) {
		case []byte:
			return append([]byte(nil), typed...)
		default:
			return typed
		}
	})
	set, err := key.Set(New(), []byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := key.Get(set)
	if !ok {
		t.Fatal("interface property was not returned")
	}
	value.([]byte)[0] = 9
	again, ok := key.Get(set)
	if !ok || again.([]byte)[0] != 1 {
		t.Fatal("interface property clone was not applied")
	}
}

func TestPropertyTypeAndUnknownLookup(t *testing.T) {
	rate := Define[sampleRateID](Scalar[int]())
	if _, err := New().With(rate, "48000"); err == nil {
		t.Fatal("wrong property type accepted")
	}
	set, _ := rate.Set(New(), 44100)
	unknown := Define[unknownPropertyID](Scalar[int]())
	if _, ok := set.Lookup(unknown.ID()); ok {
		t.Fatal("unknown property unexpectedly present")
	}
}

func TestPropertyRequiresCanonicalEncoderAndRejectsNonFiniteFloat(t *testing.T) {
	type missingEncoderID struct{}
	missing := Define[missingEncoderID, int](nil)
	if missing.Valid() || missing.Problem() == nil {
		t.Fatalf("missing encoder = valid %v, problem %v", missing.Valid(), missing.Problem())
	}
	float := Define[sampleRateID](Scalar[float64]())
	if _, err := float.Canonical(math.Inf(1)); err == nil {
		t.Fatal("non-finite property value accepted")
	}
}
