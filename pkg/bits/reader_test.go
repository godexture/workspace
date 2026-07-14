package bits

import (
	"io"
	"testing"
)

func TestBitsBasic(t *testing.T) {
	// 0xB5 = 1011 0101
	r := New([]byte{0xB5})
	if got := r.Bit(); got != 1 {
		t.Fatalf("Bit() #1 = %d, want 1", got)
	}
	if got := r.Bits64(0); got != 0 {
		t.Fatalf("Bits64(0) = %b, want 0", got)
	}
	if got := r.Bits64(3); got != 0b011 {
		t.Fatalf("Bits64(3) = %b, want 011", got)
	}
	if got := r.Bits64(4); got != 0b0101 {
		t.Fatalf("Bits64(4) = %b, want 0101", got)
	}
	if r.Overrun() {
		t.Fatalf("Overrun() = true, want false after exact consumption")
	}
}

func TestUnary64(t *testing.T) {
	// 0001 0000 -> three zero bits then a 1
	r := New([]byte{0b00010000})
	if got := r.Unary64(); got != 3 {
		t.Fatalf("Unary64() = %d, want 3", got)
	}
}

func TestUnary64StopsAtLimitWithoutHanging(t *testing.T) {
	// All-zero data with no stop bit: Unary64 must terminate, not loop forever.
	r := New([]byte{0x00, 0x00})
	got := r.Unary64()
	if got != 16 {
		t.Fatalf("Unary64() = %d, want 16 (all bits consumed)", got)
	}
	if !r.Overrun() {
		t.Fatalf("Overrun() = false, want true after exhausting data with no stop bit")
	}
}

func TestSigned32(t *testing.T) {
	// 4-bit two's complement -1 = 1111
	r := New([]byte{0b1111_0000})
	if got := r.Signed32(4); got != -1 {
		t.Fatalf("Signed32(4) = %d, want -1", got)
	}
	if got := r.Signed32(0); got != 0 {
		t.Fatalf("Signed32(0) = %d, want 0", got)
	}
}

func TestSigned64(t *testing.T) {
	// 4-bit two's complement -1 = 1111
	r := New([]byte{0b1111_0000})
	if got := r.Signed64(4); got != -1 {
		t.Fatalf("Signed64(4) = %d, want -1", got)
	}
	if got := r.Signed64(0); got != 0 {
		t.Fatalf("Signed64(0) = %d, want 0", got)
	}
}

func TestByteAtOutOfRange(t *testing.T) {
	r := New([]byte{0x42})
	if got := r.ByteAt(0); got != 0x42 {
		t.Fatalf("ByteAt(0) = %#x, want 0x42", got)
	}
	if got := r.ByteAt(1); got != 0 {
		t.Fatalf("ByteAt(1) = %#x, want 0 (out of range)", got)
	}
	if got := r.ByteAt(-1); got != 0 {
		t.Fatalf("ByteAt(-1) = %#x, want 0 (out of range)", got)
	}
}

func TestByteAndSkipToByte(t *testing.T) {
	r := New([]byte{0xAA, 0xBB})
	if got := r.Byte(); got != 0xAA {
		t.Fatalf("Byte() = %#x, want 0xAA", got)
	}
	r2 := New([]byte{0xAA, 0xBB})
	r2.Bits64(3) // move off byte alignment
	r2.SkipToByte()
	if got := r2.Byte(); got != 0xBB {
		t.Fatalf("Byte() after SkipToByte() = %#x, want 0xBB", got)
	}
}

func TestOverrunIsSticky(t *testing.T) {
	r := New([]byte{0xFF})
	r.Bits64(8) // consumes all 8 bits, exactly at limit
	if r.Overrun() {
		t.Fatalf("Overrun() = true after exact consumption, want false")
	}
	_ = r.Bit() // reads past the limit
	if !r.Overrun() {
		t.Fatalf("Overrun() = false after reading past limit, want true")
	}
	// Subsequent reads must keep reporting overrun (monotonic position).
	_ = r.Bits64(4)
	if !r.Overrun() {
		t.Fatalf("Overrun() = false on a later read, want true (sticky)")
	}
}

func TestBits32MatchesGetBitsSemantics(t *testing.T) {
	data := []byte{0b10110100, 0b11001010, 0b01010101}
	r := &Reader{}
	r.Init(data, 0, int32(len(data))*8)

	if got := r.Bits32(4); got != 0b1011 {
		t.Fatalf("Bits32(4) = %b, want 1011", got)
	}
	if got := r.Bits32(8); got != 0b0100_1100 {
		t.Fatalf("Bits32(8) = %b, want 01001100", got)
	}
}

func TestBits32ZeroFillPastLimit(t *testing.T) {
	data := []byte{0xFF}
	r := &Reader{}
	r.Init(data, 0, int32(len(data))*8)
	r.Bits32(8) // consume exactly the buffer
	if r.Overrun() {
		t.Fatalf("Overrun() = true after exact consumption, want false")
	}
	if got := r.Bits32(8); got != 0 {
		t.Fatalf("Bits32(8) past limit = %d, want 0 (zero-filled, no error)", got)
	}
	if !r.Overrun() {
		t.Fatalf("Overrun() = false after reading past limit via Bits32, want true")
	}
}

func TestSeek(t *testing.T) {
	r := New([]byte{0xFF, 0xFF})
	r.Seek(8)
	if r.Position() != 8 {
		t.Fatalf("Position() = %d, want 8", r.Position())
	}
	if r.Overrun() {
		t.Fatalf("Overrun() = true after in-range Seek, want false")
	}
	r.Seek(100)
	if !r.Overrun() {
		t.Fatalf("Overrun() = false after Seek past limit, want true")
	}
}

func TestRemainingAndUnread(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	r := New(data)
	if got := r.Remaining(); got != 24 {
		t.Fatalf("Remaining() = %d, want 24", got)
	}
	r.Bits64(8)
	if got := r.Remaining(); got != 16 {
		t.Fatalf("Remaining() after 8 bits = %d, want 16", got)
	}
	if got := r.Unread(); len(got) != 2 || got[0] != 0x02 || got[1] != 0x03 {
		t.Fatalf("Unread() = %v, want [0x02 0x03]", got)
	}
}

func TestBytePos(t *testing.T) {
	r := New([]byte{0xFF, 0xFF})
	r.Bits64(3)
	if got := r.BytePos(); got != 1 {
		t.Fatalf("BytePos() after 3 bits = %d, want 1 (rounds up)", got)
	}
}

func TestCheckedTierReturnsErrorOnOverrun(t *testing.T) {
	r := New([]byte{0xFF})
	if _, err := r.ReadBits64(8); err != nil {
		t.Fatalf("ReadBits64(8) within limit returned error: %v", err)
	}
	if _, err := r.ReadBit(); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadBit() past limit = %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := r.ReadBits64(1); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadBits64(1) past limit = %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := r.ReadSigned32(1); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadSigned32(1) past limit = %v, want io.ErrUnexpectedEOF", err)
	}
	if _, err := r.ReadUnary64(); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadUnary64() past limit = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadByteRequiresAlignmentAssertion(t *testing.T) {
	r := New([]byte{0xFF, 0xFF})
	r.Bits64(3)
	defer func() {
		if recover() == nil {
			t.Fatalf("Byte() on a non-byte-aligned reader did not panic")
		}
	}()
	r.Byte()
}

func TestReadByte(t *testing.T) {
	r := New([]byte{0x11, 0x22})
	b, err := r.ReadByte()
	if err != nil || b != 0x11 {
		t.Fatalf("ReadByte() = (%#x, %v), want (0x11, nil)", b, err)
	}
	b, err = r.ReadByte()
	if err != nil || b != 0x22 {
		t.Fatalf("ReadByte() #2 = (%#x, %v), want (0x22, nil)", b, err)
	}
	if _, err := r.ReadByte(); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadByte() past end = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestBits64UnalignedSpansMatchBitByBit exercises the batched Bits64 fast
// path (spans of 1-9 bytes, every starting bit offset) against a reference
// bit-at-a-time decode, so the byte-batching arithmetic in reader.go cannot
// silently drift from the original one-bit-per-call semantics.
func TestBits64UnalignedSpansMatchBitByBit(t *testing.T) {
	data := make([]byte, 16)
	for i := range data {
		data[i] = byte(0x9A + i*0x2F)
	}
	for offset := int32(0); offset < 8; offset++ {
		for width := uint8(1); width <= 64; width++ {
			if offset+int32(width) > int32(len(data))*8 {
				continue
			}
			r := New(data)
			r.Seek(offset)
			got := r.Bits64(width)

			ref := New(data)
			ref.Seek(offset)
			var want uint64
			for i := uint8(0); i < width; i++ {
				want = (want << 1) | uint64(ref.Bit())
			}
			if got != want {
				t.Fatalf("Bits64(width=%d) at offset %d = %#x, want %#x", width, offset, got, want)
			}
			if r.Position() != ref.Position() {
				t.Fatalf("Position() after Bits64(width=%d) at offset %d = %d, want %d", width, offset, r.Position(), ref.Position())
			}
			if r.Overrun() {
				t.Fatalf("Overrun() = true for an in-bounds Bits64(width=%d) at offset %d", width, offset)
			}
		}
	}
}

// TestUnary64FastScanMatchesBitByBit exercises the byte-skipping Unary64
// fast path across zero runs that start at every bit offset and span
// several bytes, so the LeadingZeros8-based scan cannot silently diverge
// from the bit-at-a-time semantics it replaces.
func TestUnary64FastScanMatchesBitByBit(t *testing.T) {
	for zeros := 0; zeros <= 40; zeros++ {
		for offset := 0; offset < 8; offset++ {
			totalBits := offset + zeros + 1 + 8 // padding after the stop bit
			data := make([]byte, (totalBits+7)/8)
			stopBit := offset + zeros
			data[stopBit/8] |= 1 << uint(7-stopBit%8)

			r := New(data)
			r.Seek(int32(offset))
			if got := r.Unary64(); got != uint64(zeros) {
				t.Fatalf("Unary64() with %d leading zero bits at offset %d = %d, want %d", zeros, offset, got, zeros)
			}
			if want := int32(stopBit + 1); r.Position() != want {
				t.Fatalf("Position() after Unary64() with %d zeros at offset %d = %d, want %d", zeros, offset, r.Position(), want)
			}
			if r.Overrun() {
				t.Fatalf("Overrun() = true for an in-bounds Unary64() with %d zeros at offset %d", zeros, offset)
			}
		}
	}
}

// TestUnary64FastScanOverrunMatchesBitByBit checks the sticky-overrun exit
// (no stop bit before the limit) for zero runs that end mid-byte, at the
// start of the byte-scan, and after several fully-scanned bytes.
func TestUnary64FastScanOverrunMatchesBitByBit(t *testing.T) {
	for _, totalBits := range []int{1, 3, 7, 8, 9, 15, 16, 17, 23, 31} {
		for offset := 0; offset < 8 && offset < totalBits; offset++ {
			data := make([]byte, (totalBits+7)/8) // all zero bits, no stop bit
			r := New(data)
			r.Init(data, int32(offset), int32(totalBits))
			want := uint64(totalBits - offset)
			if got := r.Unary64(); got != want {
				t.Fatalf("Unary64() overrun with limit=%d offset=%d = %d, want %d", totalBits, offset, got, want)
			}
			if !r.Overrun() {
				t.Fatalf("Overrun() = false after exhausting limit=%d offset=%d, want true", totalBits, offset)
			}
			if r.Position() != int32(totalBits) {
				t.Fatalf("Position() after overrun with limit=%d offset=%d = %d, want %d", totalBits, offset, r.Position(), totalBits)
			}
		}
	}
}

func BenchmarkBits64(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i * 37)
	}
	r := &Reader{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Init(data, 0, int32(len(data))*8)
		for r.Remaining() >= 9 {
			r.Bits64(9)
		}
	}
}

func BenchmarkReadBits64(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i * 37)
	}
	r := &Reader{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Init(data, 0, int32(len(data))*8)
		for r.Remaining() >= 9 {
			if _, err := r.ReadBits64(9); err != nil {
				break
			}
		}
	}
}

func BenchmarkBits32(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i * 37)
	}
	r := &Reader{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Init(data, 0, int32(len(data))*8)
		for r.Remaining() >= 9 {
			r.Bits32(9)
		}
	}
}

var benchByteAtSink uint32

func BenchmarkByteAt(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i * 37)
	}
	r := New(data)
	b.ResetTimer()
	var sum uint32
	for i := 0; i < b.N; i++ {
		sum += r.ByteAt(i % len(data))
	}
	benchByteAtSink = sum // store to a package-level var so the loop isn't eliminated
}
