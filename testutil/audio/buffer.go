package audio

import (
	"fmt"
	"io"
)

// Buffer is an in-memory implementation of io.ReadWriteSeeker.
type Buffer struct {
	buf []byte
	off int64
}

// NewBuffer creates a new Buffer with the given initial contents.
func NewBuffer(data []byte) *Buffer {
	if data == nil {
		return &Buffer{}
	}
	buf := make([]byte, len(data))
	copy(buf, data)
	return &Buffer{buf: buf}
}

// Write writes p to the buffer at the current offset.
func (b *Buffer) Write(p []byte) (n int, err error) {
	end := b.off + int64(len(p))
	if end > int64(len(b.buf)) {
		newBuf := make([]byte, end)
		copy(newBuf, b.buf)
		b.buf = newBuf
	}
	copy(b.buf[b.off:], p)
	b.off = end
	return len(p), nil
}

// Read reads up to len(p) bytes from the buffer at the current offset.
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.off >= int64(len(b.buf)) {
		return 0, io.EOF
	}
	n = copy(p, b.buf[b.off:])
	b.off += int64(n)
	return n, nil
}

// Seek sets the offset for the next Read or Write.
func (b *Buffer) Seek(offset int64, whence int) (int64, error) {
	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = b.off + offset
	case io.SeekEnd:
		newOff = int64(len(b.buf)) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if newOff < 0 {
		return 0, fmt.Errorf("negative position: %d", newOff)
	}
	b.off = newOff
	return newOff, nil
}

// Bytes returns the underlying buffer contents.
func (b *Buffer) Bytes() []byte {
	return b.buf
}

// Reset resets the buffer to be empty, with offset 0.
func (b *Buffer) Reset() {
	b.buf = nil
	b.off = 0
}
