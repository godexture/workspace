package internal

import (
	"errors"
	"fmt"
	"io"
)

type bitReader struct {
	data []byte
	pos  uint64
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (r *bitReader) bitPos() uint64 { return r.pos }

func (r *bitReader) bytePos() int { return int((r.pos + 7) / 8) }

func (r *bitReader) bitsRemaining() uint64 {
	total := uint64(len(r.data)) * 8
	if r.pos >= total {
		return 0
	}
	return total - r.pos
}

func (r *bitReader) readBit() (uint8, error) {
	if r.bitsRemaining() == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	byteIndex := r.pos / 8
	bitOffset := 7 - (r.pos % 8)
	r.pos++
	return (r.data[byteIndex] >> bitOffset) & 1, nil
}

func (r *bitReader) readBits(n uint8) (uint64, error) {
	if n > 64 {
		return 0, fmt.Errorf("cannot read %d bits", n)
	}
	if r.bitsRemaining() < uint64(n) {
		return 0, io.ErrUnexpectedEOF
	}
	var value uint64
	for i := uint8(0); i < n; i++ {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		value = (value << 1) | uint64(bit)
	}
	return value, nil
}

func (r *bitReader) readSigned(bits uint8) (int32, error) {
	if bits == 0 || bits > 32 {
		return 0, fmt.Errorf("invalid signed bit width: %d", bits)
	}
	value, err := r.readBits(bits)
	if err != nil {
		return 0, err
	}
	if value&(uint64(1)<<(bits-1)) != 0 {
		value |= ^uint64(0) << bits
	}
	return int32(value), nil
}

func (r *bitReader) readUnary() (uint64, error) {
	var count uint64
	for {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			return count, nil
		}
		count++
	}
}

func (r *bitReader) alignByte() {
	if rem := r.pos % 8; rem != 0 {
		r.pos += 8 - rem
	}
}

func (r *bitReader) readByteAligned() (byte, error) {
	if r.pos%8 != 0 {
		return 0, errors.New("bit reader is not byte aligned")
	}
	if r.bitsRemaining() < 8 {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos/8]
	r.pos += 8
	return b, nil
}
