package bits

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

// the limit. It is a sticky flag reset by Init/New: since the read position
// only advances, once it goes true every later read is also out of range,
// so a single check after a batch of Fast-tier reads is equivalent to
// checking every individual call.
func (r *Reader) Overrun() bool { return r.overrun }
