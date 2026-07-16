package bits

import "testing"

func TestWriterBit(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	// Write 1011 0101 = 0xB5
	for _, b := range []uint8{1, 0, 1, 1, 0, 1, 0, 1} {
		w.Bit(b)
	}
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0xB5 {
		t.Fatalf("Bytes() = % x, want [b5]", got)
	}
	if w.Position() != 8 {
		t.Fatalf("Position() = %d, want 8", w.Position())
	}
}

func TestWriterBits64(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bits64(0b1011, 4)
	w.Bits64(0b0101, 4)
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0b1011_0101 {
		t.Fatalf("Bytes() = % x, want [b5]", got)
	}
}

func TestWriterByteRequiresAlignment(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bit(1)
	defer func() {
		if recover() == nil {
			t.Fatalf("Byte() on a non-byte-aligned writer did not panic")
		}
	}()
	w.Byte(0xFF)
}

func TestWriterByteAndBytesAppend(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Byte(0xAA)
	w.BytesAppend([]byte{0xBB, 0xCC})
	got := w.Bytes()
	want := []byte{0xAA, 0xBB, 0xCC}
	if len(got) != len(want) {
		t.Fatalf("Bytes() = % x, want % x", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Bytes() = % x, want % x", got, want)
		}
	}
}

func TestWriterUnary64(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Unary64(3) // 0001
	w.PadToByte()
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0b0001_0000 {
		t.Fatalf("Bytes() = % x, want [10]", got)
	}
}

func TestWriterPadToByte(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bits64(0b101, 3)
	w.PadToByte()
	if w.Position() != 8 {
		t.Fatalf("Position() = %d, want 8", w.Position())
	}
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0b1010_0000 {
		t.Fatalf("Bytes() = % x, want [a0]", got)
	}
	// PadToByte on an already aligned writer is a no-op.
	w.PadToByte()
	if w.Position() != 8 {
		t.Fatalf("Position() after aligned PadToByte = %d, want 8", w.Position())
	}
}

// TestWriterReaderRoundtrip verifies that values written by Writer are read
// back identically by Reader across bit, signed, and unary encodings.
func TestWriterReaderRoundtrip(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bit(1)
	w.Bits64(0x2ab, 12)
	w.Signed64(-5, 6)
	w.Unary64(7)
	w.Signed64(123456, 24)
	w.PadToByte()

	r := New(w.Bytes())
	if got := r.Bit(); got != 1 {
		t.Fatalf("Bit() = %d, want 1", got)
	}
	if got := r.Bits64(12); got != 0x2ab {
		t.Fatalf("Bits64(12) = %#x, want 0x2ab", got)
	}
	if got := r.Signed64(6); got != -5 {
		t.Fatalf("Signed64(6) = %d, want -5", got)
	}
	if got := r.Unary64(); got != 7 {
		t.Fatalf("Unary64() = %d, want 7", got)
	}
	if got := r.Signed64(24); got != 123456 {
		t.Fatalf("Signed64(24) = %d, want 123456", got)
	}
	if r.Overrun() {
		t.Fatalf("Overrun() = true, want false after exact roundtrip")
	}
}

func TestWriterInitReuse(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bits64(0xFF, 8)
	w.Init()
	if w.Position() != 0 {
		t.Fatalf("Position() after Init = %d, want 0", w.Position())
	}
	if len(w.Bytes()) != 0 {
		t.Fatalf("Bytes() after Init = % x, want empty", w.Bytes())
	}
	w.Bits64(0x42, 8)
	got := w.Bytes()
	if len(got) != 1 || got[0] != 0x42 {
		t.Fatalf("Bytes() after reuse = % x, want [42]", got)
	}
}

// bitByBitWriter mirrors the pre-batching Bits64/Unary64 implementations
// (single-bit Bit() calls only), used as a reference oracle below.
type bitByBitWriter struct{ w *Writer }

func (b bitByBitWriter) bits64(value uint64, width uint8) {
	for i := int(width) - 1; i >= 0; i-- {
		b.w.Bit(uint8((value >> uint(i)) & 1))
	}
}

func (b bitByBitWriter) unary64(value uint64) {
	for i := uint64(0); i < value; i++ {
		b.w.Bit(0)
	}
	b.w.Bit(1)
}

// TestWriterBits64UnalignedMatchesBitByBit exercises the batched Bits64
// fast path at every starting bit offset and width, checking its output is
// byte-for-byte identical to the original per-bit implementation.
func TestWriterBits64UnalignedMatchesBitByBit(t *testing.T) {
	t.Parallel()
	for offset := uint8(0); offset < 8; offset++ {
		for width := uint8(1); width <= 64; width++ {
			value := uint64(0x9E3779B97F4A7C15) >> (64 - width)

			got := NewWriter()
			for i := uint8(0); i < offset; i++ {
				got.Bit(0)
			}
			got.Bits64(value, width)

			want := NewWriter()
			ref := bitByBitWriter{want}
			for i := uint8(0); i < offset; i++ {
				want.Bit(0)
			}
			ref.bits64(value, width)

			if got.Position() != want.Position() {
				t.Fatalf("offset=%d width=%d: Position() = %d, want %d", offset, width, got.Position(), want.Position())
			}
			if string(got.Bytes()) != string(want.Bytes()) {
				t.Fatalf("offset=%d width=%d: Bytes() = %08b, want %08b", offset, width, got.Bytes(), want.Bytes())
			}
		}
	}
}

// TestWriterUnary64UnalignedMatchesBitByBit exercises the batched Unary64
// fast path across zero-run lengths and starting bit offsets, checking its
// output is byte-for-byte identical to the original per-bit implementation.
func TestWriterUnary64UnalignedMatchesBitByBit(t *testing.T) {
	t.Parallel()
	for offset := uint8(0); offset < 8; offset++ {
		for value := uint64(0); value <= 40; value++ {
			got := NewWriter()
			for i := uint8(0); i < offset; i++ {
				got.Bit(0)
			}
			got.Unary64(value)

			want := NewWriter()
			ref := bitByBitWriter{want}
			for i := uint8(0); i < offset; i++ {
				want.Bit(0)
			}
			ref.unary64(value)

			if got.Position() != want.Position() {
				t.Fatalf("offset=%d value=%d: Position() = %d, want %d", offset, value, got.Position(), want.Position())
			}
			if string(got.Bytes()) != string(want.Bytes()) {
				t.Fatalf("offset=%d value=%d: Bytes() = %08b, want %08b", offset, value, got.Bytes(), want.Bytes())
			}
		}
	}
}

func TestWriterBytePos(t *testing.T) {
	t.Parallel()
	w := NewWriter()
	w.Bits64(0b101, 3)
	if got := w.BytePos(); got != 1 {
		t.Fatalf("BytePos() after 3 bits = %d, want 1", got)
	}
}
