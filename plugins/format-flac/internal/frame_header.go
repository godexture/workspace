package internal

import (
	"errors"
	"fmt"
)

type frameHeader struct {
	blockSize int
	fixed     bool
}

func parseFrameHeader(data []byte) (frameHeader, error) {
	if len(data) < 5 {
		return frameHeader{}, errors.New("FLAC frame header is truncated")
	}
	if uint16(data[0])<<6|uint16(data[1])>>2 != 0x3ffe {
		return frameHeader{}, errors.New("invalid FLAC frame sync")
	}
	if data[1]&0x02 != 0 || data[3]&0x01 != 0 {
		return frameHeader{}, errors.New("invalid FLAC frame header reserved bit")
	}

	fixed := data[1]&0x01 == 0
	blockSizeCode := data[2] >> 4
	sampleRateCode := data[2] & 0x0f
	if blockSizeCode == 0 {
		return frameHeader{}, errors.New("reserved FLAC block size code")
	}
	if sampleRateCode == 15 {
		return frameHeader{}, errors.New("reserved FLAC sample rate code")
	}

	pos, number, err := skipUTF8CodedNumber(data, 4)
	if err != nil {
		return frameHeader{}, fmt.Errorf("decode FLAC frame number: %w", err)
	}
	if fixed && number > 0x7fffffff {
		return frameHeader{}, errors.New("FLAC fixed-blocking frame number exceeds 31 bits")
	}

	blockSize, extraBytes, err := frameBlockSize(blockSizeCode)
	if err != nil {
		return frameHeader{}, err
	}
	if len(data) < pos+extraBytes {
		return frameHeader{}, errors.New("FLAC frame block size is truncated")
	}
	switch blockSizeCode {
	case 6:
		blockSize = int(data[pos]) + 1
	case 7:
		blockSize = (int(data[pos])<<8 | int(data[pos+1])) + 1
	}
	return frameHeader{blockSize: blockSize, fixed: fixed}, nil
}

func skipUTF8CodedNumber(data []byte, pos int) (int, uint64, error) {
	if pos >= len(data) {
		return 0, 0, errors.New("FLAC UTF-8 coded number is truncated")
	}
	first := data[pos]
	pos++
	if first&0x80 == 0 {
		return pos, uint64(first), nil
	}

	length := 0
	for mask := byte(0x80); first&mask != 0; mask >>= 1 {
		length++
	}
	if length < 2 || length > 7 || len(data) < pos+length-1 {
		return 0, 0, errors.New("invalid FLAC UTF-8 coded number")
	}
	value := uint64(first & (0xff >> (length + 1)))
	for range length - 1 {
		b := data[pos]
		pos++
		if b&0xc0 != 0x80 {
			return 0, 0, errors.New("invalid FLAC UTF-8 continuation byte")
		}
		value = value<<6 | uint64(b&0x3f)
	}
	minimum := []uint64{0, 0, 0x80, 0x800, 0x10000, 0x200000, 0x4000000, 0x80000000}[length]
	if value < minimum || value > 0xfffffffff {
		return 0, 0, errors.New("non-canonical or oversized FLAC UTF-8 coded number")
	}
	return pos, value, nil
}

func frameBlockSize(code byte) (int, int, error) {
	switch code {
	case 1:
		return 192, 0, nil
	case 2, 3, 4, 5:
		return 576 << (code - 2), 0, nil
	case 6:
		return 0, 1, nil
	case 7:
		return 0, 2, nil
	case 8, 9, 10, 11, 12, 13, 14, 15:
		return 256 << (code - 8), 0, nil
	default:
		return 0, 0, errors.New("reserved FLAC block size code")
	}
}
