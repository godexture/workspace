package sample

import "testing"

func TestCodingWidthsAndCanonicalDecoding(t *testing.T) {
	cases := []struct {
		coding  Coding
		bytes   int
		float   bool
		decoded Coding
	}{
		{U8, 1, false, S16},
		{S8, 1, false, S16},
		{S16, 2, false, S16},
		{S24, 3, false, S32},
		{S32, 4, false, S32},
		{F32, 4, true, F32},
		{F64, 8, true, F64},
	}
	for _, value := range cases {
		if got := value.coding.Bytes(); got != value.bytes {
			t.Errorf("%s bytes = %d, want %d", value.coding, got, value.bytes)
		}
		if got := value.coding.Bits(); got != value.bytes*8 {
			t.Errorf("%s bits = %d", value.coding, got)
		}
		if got := value.coding.Float(); got != value.float {
			t.Errorf("%s float = %v", value.coding, got)
		}
		if got := value.coding.Decoded(); got != value.decoded {
			t.Errorf("%s decoded = %s, want %s", value.coding, got, value.decoded)
		}
		if !value.coding.Valid() {
			t.Errorf("%s is not valid", value.coding)
		}
	}
}

// A decoded frame has one schema per Go scalar type, so exactly four codings
// may name a planar stream. Widening keeps that set closed no matter how many
// wire codings are added later.
func TestOnlyFourCodingsAreCanonical(t *testing.T) {
	canonical := 0
	for _, coding := range []Coding{U8, S8, S16, S24, S32, F32, F64} {
		if coding.Canonical() {
			canonical++
		}
		if !coding.Decoded().Canonical() {
			t.Errorf("%s decodes into non-canonical %s", coding, coding.Decoded())
		}
	}
	if canonical != 4 {
		t.Fatalf("canonical codings = %d, want 4", canonical)
	}
}

func TestUnknownVocabularyIsRejected(t *testing.T) {
	if Coding("s20").Valid() || Coding("").Valid() || Coding("s20").Decoded() != "" {
		t.Error("unknown coding accepted")
	}
	if Packing("mixed").Valid() || Packing("").Valid() {
		t.Error("unknown packing accepted")
	}
	if Endian("middle").Valid() || !NoEndian.Valid() {
		t.Error("unknown endian accepted")
	}
}
