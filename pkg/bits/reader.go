package bits

import "io"

// Reader reads bits from a byte buffer. It exposes two API tiers:
//
//   - Fast tier (no "Read" prefix): never returns an error. Reads past the
//     end of the buffer are zero-filled/clamped and recorded via Overrun.
//     Intended for hot decode loops.
//   - Checked tier ("Read" prefix): thin wrappers around the Fast tier that
//     surface Overrun as an error. Intended for cold paths (header parsing)
//     where a precise per-call error is worth the branch.
type Reader struct {
	buffer   []byte
	position int32
	limit    int32
	overrun  bool
}

// New creates a Reader over data, starting at bit position 0.
func New(data []byte) *Reader {
	r := &Reader{}
	r.Init(data, 0, int32(len(data))*8)
	return r
}

// Init (re)initializes the reader without allocating, so it can be reused
// across frames.
func (r *Reader) Init(buffer []byte, position int32, limit int32) {
	r.buffer = buffer
	r.position = position
	r.limit = limit
	r.overrun = false
}

// Position returns the current bit position.
func (r *Reader) Position() int32 { return r.position }

// BytePos returns the current position rounded up to the next byte.
func (r *Reader) BytePos() int { return int((r.position + 7) / 8) }

// Seek jumps to an absolute bit position. It exists for the one place that
// needs it (Huffman decoding aligning to a granule's declared bit boundary),
// not as a general-purpose field setter.
func (r *Reader) Seek(pos int32) {
	if pos > r.limit {
		r.overrun = true
	}
	r.position = pos
}

// Remaining returns the number of unread bits between the current position
// and the limit.
func (r *Reader) Remaining() int32 {
	if r.position >= r.limit {
		return 0
	}
	return r.limit - r.position
}

// Unread returns the unread byte slice between the current (byte-aligned)
// position and the limit.
func (r *Reader) Unread() []byte {
	start := r.BytePos()
	end := int(r.limit / 8)
	if start < 0 {
		start = 0
	}
	if start > len(r.buffer) {
		start = len(r.buffer)
	}
	if end > len(r.buffer) {
		end = len(r.buffer)
	}
	if end < start {
		return nil
	}
	return r.buffer[start:end]
}

// ByteAt returns the byte at index pos, or 0 if pos is out of range. It is the
// bounds-checked primitive shared by Bit/Byte/Bits32 and by MP3's Huffman
// decoding hot path.
func (r *Reader) ByteAt(pos int) uint32 {
	if pos < 0 || pos >= len(r.buffer) {
		return 0
	}
	return uint32(r.buffer[pos])
}

// Bit reads a single bit (0 or 1). Past the limit it sets Overrun and
// returns 0. It returns uint8 rather than bool so Bits64/Unary64/Signed32
// can accumulate it with a branchless shift-or instead of a conditional.
func (r *Reader) Bit() uint8 {
	if r.position >= r.limit {
		r.overrun = true
		r.position++
		return 0
	}
	byteIndex := int(r.position / 8)
	bitOffset := 7 - (r.position % 8)
	r.position++
	return uint8((r.ByteAt(byteIndex) >> uint(bitOffset)) & 1)
}

// Byte reads one byte at the current (byte-aligned) position. Alignment is
// a caller invariant, not a data-driven condition, so it is checked via
// assertf rather than returned as an error.
func (r *Reader) Byte() uint8 {
	assertf(r.position%8 == 0, "bits: Byte called on a non-byte-aligned reader (position=%d)", r.position)
	if r.position+8 > r.limit {
		r.overrun = true
	}
	b := r.ByteAt(int(r.position / 8))
	r.position += 8
	return uint8(b)
}

// Bits64 reads width bits (width <= 64) MSB-first by calling Bit width times.
func (r *Reader) Bits64(width uint8) uint64 {
	assertf(width <= 64, "bits: Bits64 width out of range: %d", width)
	var value uint64
	for i := uint8(0); i < width; i++ {
		value = (value << 1) | uint64(r.Bit())
	}
	return value
}

// Unary64 reads a unary-coded value: the number of 0 bits before the next 1
// bit. It stops at the limit instead of looping forever on truncated data.
func (r *Reader) Unary64() uint64 {
	var count uint64
	for r.position < r.limit {
		if r.Bit() == 1 {
			return count
		}
		count++
	}
	r.overrun = true
	return count
}

// Signed32 reads width bits (width in [1, 32]) and sign-extends the result.
func (r *Reader) Signed32(width uint8) int32 {
	assertf(width > 0 && width <= 32, "bits: Signed32 width out of range: %d", width)
	value := r.Bits64(width)
	if value&(uint64(1)<<(width-1)) != 0 {
		value |= ^uint64(0) << width
	}
	return int32(value)
}

// SkipToByte advances the position to the next byte boundary without
// reading a value. This is a distinct operation from Byte/ReadByte (which
// read a byte), kept separate so the two are never confused for one another.
func (r *Reader) SkipToByte() {
	if rem := r.position % 8; rem != 0 {
		r.position += 8 - rem
	}
}

// Bits32 reads a width up to 32 bits using a byte-cache algorithm ported
// unchanged from MP3's original GetBits (used by codec-mp3's layer1/2
// dequantization hot path). It is not decomposed into Bit, since doing so
// would change the performance characteristics MP3 currently relies on.
func (r *Reader) Bits32(width uint8) uint32 {
	bitOffset := r.position & 7
	shiftLeft := int32(width) + bitOffset
	byteIndex := int(r.position >> 3)
	r.position += int32(width)
	if r.position > r.limit {
		r.overrun = true
		return 0
	}
	next := r.ByteAt(byteIndex) & (255 >> bitOffset)
	byteIndex++
	bitCache := uint32(0)
	for shiftLeft > 8 {
		shiftLeft -= 8
		bitCache |= next << shiftLeft
		next = r.ByteAt(byteIndex)
		byteIndex++
	}
	shiftLeft -= 8
	return bitCache | (next >> -shiftLeft)
}

// Overrun reports whether any Fast-tier method has attempted to read past
// the limit. It is a sticky flag reset by Init/New: since the read position
// only advances, once it goes true every later read is also out of range,
// so a single check after a batch of Fast-tier reads is equivalent to
// checking every individual call.
func (r *Reader) Overrun() bool { return r.overrun }

// ReadBit is the Checked-tier wrapper around Bit.
func (r *Reader) ReadBit() (uint8, error) {
	v := r.Bit()
	if r.Overrun() {
		return 0, io.ErrUnexpectedEOF
	}
	return v, nil
}

// ReadByte is the Checked-tier wrapper around Byte, named to match Go's
// io.ByteReader convention (e.g. bufio.Reader.ReadByte).
func (r *Reader) ReadByte() (byte, error) {
	v := r.Byte()
	if r.Overrun() {
		return 0, io.ErrUnexpectedEOF
	}
	return v, nil
}

// ReadBits64 is the Checked-tier wrapper around Bits64.
func (r *Reader) ReadBits64(width uint8) (uint64, error) {
	v := r.Bits64(width)
	if r.Overrun() {
		return 0, io.ErrUnexpectedEOF
	}
	return v, nil
}

// ReadSigned32 is the Checked-tier wrapper around Signed32.
func (r *Reader) ReadSigned32(width uint8) (int32, error) {
	v := r.Signed32(width)
	if r.Overrun() {
		return 0, io.ErrUnexpectedEOF
	}
	return v, nil
}

// ReadUnary64 is the Checked-tier wrapper around Unary64.
func (r *Reader) ReadUnary64() (uint64, error) {
	v := r.Unary64()
	if r.Overrun() {
		return 0, io.ErrUnexpectedEOF
	}
	return v, nil
}
