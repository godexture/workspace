package bits

import (
	"io"
)

func checked[T any](r *Reader, read func() T) (T, error) {
	value := read()
	if r.Overrun() {
		var zero T
		return zero, io.ErrUnexpectedEOF
	}
	return value, nil
}

// ReadBit is the Checked-tier wrapper around Bit.
func (r *Reader) ReadBit() (uint8, error) {
	return checked(r, r.Bit)
}

// ReadByte is the Checked-tier wrapper around Byte, named to match Go's
// io.ByteReader convention (e.g. bufio.Reader.ReadByte).
func (r *Reader) ReadByte() (byte, error) {
	return checked(r, r.Byte)
}

// ReadBits64 is the Checked-tier wrapper around Bits64.
func (r *Reader) ReadBits64(width uint8) (uint64, error) {
	return checked(r, func() uint64 { return r.Bits64(width) })
}

// ReadBits32 is the Checked-tier wrapper around Bits32.
func (r *Reader) ReadBits32(width uint8) (uint32, error) {
	return checked(r, func() uint32 { return r.Bits32(width) })
}

// ReadSigned32 is the Checked-tier wrapper around Signed32.
func (r *Reader) ReadSigned32(width uint8) (int32, error) {
	return checked(r, func() int32 { return r.Signed32(width) })
}

// ReadSigned64 is the Checked-tier wrapper around Signed64.
func (r *Reader) ReadSigned64(width uint8) (int64, error) {
	return checked(r, func() int64 { return r.Signed64(width) })
}

// ReadUnary64 is the Checked-tier wrapper around Unary64.
func (r *Reader) ReadUnary64() (uint64, error) {
	return checked(r, r.Unary64)
}
