package bits

import (
	"io"
)

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

// ReadBits32 is the Checked-tier wrapper around Bits32.
func (r *Reader) ReadBits32(width uint8) (uint32, error) {
	v := r.Bits32(width)
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

func (r *Reader) ReadSigned64(width uint8) (int64, error) {
	v := r.Signed64(width)
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
