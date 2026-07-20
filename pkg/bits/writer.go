package bits

import "slices"

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

// Grow ensures space for n more bytes without another allocation.
func (w *Writer) Grow(n int) {
	w.buffer = slices.Grow(w.buffer, n)
}

// Position returns the current bit position.
func (w *Writer) Position() int32 { return w.position }

// BytePos returns the current position rounded up to the next byte.
func (w *Writer) BytePos() int { return int((w.position + 7) / 8) }

// Bytes returns the bytes written so far. If the writer is not byte-aligned,
// the final byte contains zero padding in the remaining least-significant bits.
func (w *Writer) Bytes() []byte { return w.buffer }

// DetachBytes transfers the written buffer to the caller and resets the writer.
// The returned bytes must not be retained by the writer's caller after transfer.
func (w *Writer) DetachBytes() []byte {
	buffer := w.buffer
	w.buffer = nil
	w.position = 0
	return buffer
}

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
	if w.position%8 != 0 {
		assertf(false, "bits: Byte called on a non-byte-aligned writer (position=%d)", w.position)
	}
	w.buffer = append(w.buffer, value)
	w.position += 8
}

// BytesAppend writes bytes at the current byte-aligned position.
func (w *Writer) BytesAppend(values []byte) {
	if w.position%8 != 0 {
		assertf(false, "bits: BytesAppend called on a non-byte-aligned writer (position=%d)", w.position)
	}
	w.buffer = append(w.buffer, values...)
	w.position += int32(len(values) * 8)
}

// Bits64 writes width bits (width <= 64) MSB-first from value. It merges
// into the in-progress last byte once (if the writer isn't byte-aligned),
// appends any whole bytes directly, and appends one final partial byte —
// replacing what used to be `width` separate per-bit calls, each with its
// own division/modulo and append-growth check.
func (w *Writer) Bits64(value uint64, width uint8) {
	if width > 64 {
		assertf(false, "bits: Bits64 width out of range: %d", width)
	}
	if width == 0 {
		return
	}
	if width < 64 {
		value &= (uint64(1) << width) - 1
	}
	remaining := uint(width)

	if bitOffset := uint(w.position & 7); bitOffset != 0 {
		byteIndex := int(w.position / 8)
		if byteIndex >= len(w.buffer) {
			assertf(false, "bits: Bits64 misaligned writer state (position=%d, len(buffer)=%d)", w.position, len(w.buffer))
		}
		free := 8 - bitOffset
		take := free
		if take > remaining {
			take = remaining
		}
		chunk := uint8((value >> (remaining - take)) & ((1 << take) - 1))
		w.buffer[byteIndex] |= chunk << (free - take)
		remaining -= take
		w.position += int32(take)
	}

	if remaining >= 8 {
		// remaining <= 64 here, so at most 8 whole bytes: build them on the
		// stack and append once, instead of one append call per byte.
		var wholeBytes [8]byte
		n := 0
		for remaining >= 8 {
			remaining -= 8
			wholeBytes[n] = uint8(value >> remaining)
			n++
		}
		w.buffer = append(w.buffer, wholeBytes[:n]...)
		w.position += int32(n) * 8
	}

	if remaining > 0 {
		w.buffer = append(w.buffer, uint8(value&((1<<remaining)-1))<<(8-remaining))
		w.position += int32(remaining)
	}
}

// Signed64 writes width bits (width <= 64) as a two's-complement signed value.
func (w *Writer) Signed64(value int64, width uint8) {
	if width > 64 {
		assertf(false, "bits: Signed64 width out of range: %d", width)
	}
	w.Bits64(uint64(value), width)
}

// Unary64 writes a unary-coded value: value zero bits followed by a one bit.
// Rice-coded residuals (its main caller) are dominated by this: each sample
// writes one unary quotient, so batching the zero run into whole-byte
// appends instead of `value` separate per-bit calls matters even when the
// typical run is short.
func (w *Writer) Unary64(value uint64) {
	remaining := value
	if bitOffset := uint(w.position & 7); bitOffset != 0 {
		byteIndex := int(w.position / 8)
		if byteIndex >= len(w.buffer) {
			assertf(false, "bits: Unary64 misaligned writer state (position=%d, len(buffer)=%d)", w.position, len(w.buffer))
		}
		free := uint64(8 - bitOffset)
		if remaining < free {
			w.buffer[byteIndex] |= 1 << (free - remaining - 1)
			w.position += int32(remaining) + 1
			return
		}
		remaining -= free
		w.position += int32(free)
	}

	for remaining >= 8 {
		w.buffer = append(w.buffer, 0)
		remaining -= 8
		w.position += 8
	}

	w.buffer = append(w.buffer, 1<<(7-uint(remaining)))
	w.position += int32(remaining) + 1
}

// UnaryBits64 writes a unary-prefixed bit-field: value>>width zero bits,
// then a one bit, then the low width bits of value. It is equivalent to
// Unary64(value>>width) followed by Bits64(value, width) — the pattern each
// Rice-coded residual sample uses — but folds both into a single Bits64 call
// when the combined field fits in 64 bits (the common case once the Rice
// parameter is well chosen), instead of two separate alignment/append
// passes per sample.
func (w *Writer) UnaryBits64(value uint64, width uint8) {
	quotient := value >> width
	total := uint64(width) + 1 + quotient
	if total <= 64 {
		remainder := value
		if width < 64 {
			remainder &= (uint64(1) << width) - 1
		}
		w.Bits64((uint64(1)<<width)|remainder, uint8(total))
		return
	}
	w.Unary64(quotient)
	w.Bits64(value, width)
}

// PadToByte writes zero bits until the next byte boundary.
func (w *Writer) PadToByte() {
	if rem := w.position % 8; rem != 0 {
		for i := int32(0); i < 8-rem; i++ {
			w.Bit(0)
		}
	}
}
