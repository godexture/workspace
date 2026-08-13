package buffer

import (
	"bytes"
	"io"
)

// Bytes is an immutable borrowed byte view. It never exposes or retains its
// backing slice; the originating Handle must remain valid for every use.
type Bytes struct {
	lease  *lease
	offset int
	size   int
}

func (b Bytes) Valid() bool { return b.bytes() != nil }

// Len reports the recorded range and does not revalidate the originating
// lease, so it stays cheap in loop conditions. Every read still fails or
// panics once that lease is released; use Valid to test liveness.
func (b Bytes) Len() int { return b.size }

// At reads one byte and revalidates the originating lease on every call, so it
// suits incidental access only. Read ranges through Blocks, CopyTo, or
// AppendTo instead.
func (b Bytes) At(index int) byte {
	data := b.bytes()
	if data == nil {
		panic("buffer bytes outlived their originating lease")
	}
	if index < 0 || index >= len(data) {
		panic("buffer byte index out of range")
	}
	return data[index]
}

// Slice returns a zero-copy immutable subview.
func (b Bytes) Slice(offset, size int) (Bytes, error) {
	if !b.Valid() {
		return Bytes{}, ErrInvalidHandle
	}
	if offset < 0 || size < 0 || offset > b.size || size > b.size-offset {
		return Bytes{}, ErrRange
	}
	return Bytes{lease: b.lease, offset: b.offset + offset, size: size}, nil
}

// From returns the zero-copy immutable subview that starts at offset.
func (b Bytes) From(offset int) (Bytes, error) { return b.Slice(offset, b.size-offset) }

// Blocks drains the view through scratch and calls visit once per block with
// that block and its offset in the view. The block is valid only during the
// call. This is the read path for payload-sized work: it keeps the backing
// private, allocates nothing, and costs one lifetime check per block instead
// of one per element.
func (b Bytes) Blocks(scratch []byte, visit func(block []byte, offset int) error) error {
	if !b.Valid() {
		return ErrInvalidHandle
	}
	if len(scratch) == 0 {
		return ErrRange
	}
	for offset := 0; offset < b.size; {
		count := min(len(scratch), b.size-offset)
		part, err := b.Slice(offset, count)
		if err != nil {
			return err
		}
		if part.CopyTo(scratch[:count]) != count {
			return ErrInvalidHandle
		}
		if err := visit(scratch[:count], offset); err != nil {
			return err
		}
		offset += count
	}
	return nil
}

func (b Bytes) CopyTo(destination []byte) int { return copy(destination, b.bytes()) }

func (b Bytes) AppendTo(destination []byte) []byte { return append(destination, b.bytes()...) }

// Reader returns an independent read cursor. Its concrete type implements
// only io.Reader, so it cannot hand the backing slice to another writer.
func (b Bytes) Reader() io.Reader { return &reader{view: b} }

func (b Bytes) Equal(other Bytes) bool {
	left, right := b.bytes(), other.bytes()
	return left != nil && right != nil && bytes.Equal(left, right)
}

func (b Bytes) EqualSlice(other []byte) bool {
	data := b.bytes()
	return data != nil && bytes.Equal(data, other)
}

func (b Bytes) bytes() []byte {
	if b.lease == nil || b.lease.storage == nil || b.lease.released.Load() || b.lease.storage.refs.Load() == 0 || b.offset < 0 || b.size < 0 || b.offset > b.lease.layout.Size || b.size > b.lease.layout.Size-b.offset {
		return nil
	}
	data := b.lease.bytes()
	return data[b.offset : b.offset+b.size : b.offset+b.size]
}

type reader struct {
	view   Bytes
	offset int
}

func (r *reader) Read(destination []byte) (int, error) {
	if r == nil || !r.view.Valid() {
		return 0, ErrInvalidHandle
	}
	if r.offset == r.view.size {
		return 0, io.EOF
	}
	remaining, err := r.view.From(r.offset)
	if err != nil {
		return 0, err
	}
	count := remaining.CopyTo(destination)
	r.offset += count
	return count, nil
}
