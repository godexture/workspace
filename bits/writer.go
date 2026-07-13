package bits

// Writer writes MSB-first bits to an in-memory byte buffer.
type Writer struct {
	buffer   []byte
	position int32
}

// NewWriter creates an empty bit writer.
func NewWriter() *Writer { return &Writer{} }

// Init resets the writer to an empty buffer, retaining capacity for reuse.
func (w *Writer) Init() {
	w.buffer = w.buffer[:0]
	w.position = 0
}

// Position returns the current bit position.
func (w *Writer) Position() int32 { return w.position }

// BytePos returns the current position rounded up to the next byte.
func (w *Writer) BytePos() int { return int((w.position + 7) / 8) }

// Bytes returns the bytes written so far. If the writer is not byte-aligned,
// the final byte contains zero padding in the remaining least-significant bits.
func (w *Writer) Bytes() []byte { return w.buffer }

// Bit writes a single bit. Any non-zero value is encoded as 1.
func (w *Writer) Bit(value uint8) {
	byteIndex := int(w.position / 8)
	bitOffset := 7 - uint(w.position%8)
	if byteIndex == len(w.buffer) {
		w.buffer = append(w.buffer, 0)
	}
	if value != 0 {
		w.buffer[byteIndex] |= 1 << bitOffset
	}
	w.position++
}

// Byte writes one byte at the current byte-aligned position.
func (w *Writer) Byte(value byte) {
	assertf(w.position%8 == 0, "bits: Byte called on a non-byte-aligned writer (position=%d)", w.position)
	w.buffer = append(w.buffer, value)
	w.position += 8
}

// BytesAppend writes bytes at the current byte-aligned position.
func (w *Writer) BytesAppend(values []byte) {
	assertf(w.position%8 == 0, "bits: BytesAppend called on a non-byte-aligned writer (position=%d)", w.position)
	w.buffer = append(w.buffer, values...)
	w.position += int32(len(values) * 8)
}

// Bits64 writes width bits (width <= 64) MSB-first from value.
func (w *Writer) Bits64(value uint64, width uint8) {
	assertf(width <= 64, "bits: Bits64 width out of range: %d", width)
	for i := int(width) - 1; i >= 0; i-- {
		w.Bit(uint8((value >> uint(i)) & 1))
	}
}

// Signed64 writes width bits (width <= 64) as a two's-complement signed value.
func (w *Writer) Signed64(value int64, width uint8) {
	assertf(width <= 64, "bits: Signed64 width out of range: %d", width)
	w.Bits64(uint64(value), width)
}

// Unary64 writes a unary-coded value: value zero bits followed by a one bit.
func (w *Writer) Unary64(value uint64) {
	for i := uint64(0); i < value; i++ {
		w.Bit(0)
	}
	w.Bit(1)
}

// PadToByte writes zero bits until the next byte boundary.
func (w *Writer) PadToByte() {
	if rem := w.position % 8; rem != 0 {
		for i := int32(0); i < 8-rem; i++ {
			w.Bit(0)
		}
	}
}
