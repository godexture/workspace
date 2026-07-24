package frame

import (
	"errors"
	"fmt"

	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func EncodeHeader(w *bits.Writer, header *Header, streamableSubset bool) error {
	block, err := encodeBlockSize(header.BlockSize)
	if err != nil {
		return err
	}
	rate, err := encodeSampleRate(header.SampleRate)
	if err != nil {
		return err
	}
	depth, err := encodeBitsPerSample(header.BitsPerSample)
	if err != nil {
		return err
	}
	if header.ChannelAssignment > 10 {
		return fmt.Errorf("invalid FLAC channel assignment: %d", header.ChannelAssignment)
	}
	if streamableSubset && (rate.code == 0 || depth == 0) {
		return errors.New("FLAC streamable subset requires explicit frame sample-rate and bit-depth codes")
	}
	w.Bits64(0x3ffe, 14)
	w.Bits64(0, 1)
	if header.BlockingStrategy {
		w.Bits64(1, 1)
	} else {
		w.Bits64(0, 1)
	}
	w.Bits64(uint64(block.code), 4)
	w.Bits64(uint64(rate.code), 4)
	w.Bits64(uint64(header.ChannelAssignment), 4)
	w.Bits64(uint64(depth), 3)
	w.Bits64(0, 1)
	if err := encodeUTF8Number(w, header.Number); err != nil {
		return err
	}
	if block.bits > 0 {
		w.Bits64(uint64(block.extra), block.bits)
	}
	if rate.bits > 0 {
		w.Bits64(uint64(rate.extra), rate.bits)
	}
	w.Byte(hash.CRC8(w.Bytes()))
	return nil
}

type encodedField struct {
	code  uint8
	extra uint16
	bits  uint8
}

func encodeBlockSize(size int) (encodedField, error) {
	switch size {
	case 192:
		return encodedField{code: 1}, nil
	case 576:
		return encodedField{code: 2}, nil
	case 1152:
		return encodedField{code: 3}, nil
	case 2304:
		return encodedField{code: 4}, nil
	case 4608:
		return encodedField{code: 5}, nil
	case 256:
		return encodedField{code: 8}, nil
	case 512:
		return encodedField{code: 9}, nil
	case 1024:
		return encodedField{code: 10}, nil
	case 2048:
		return encodedField{code: 11}, nil
	case 4096:
		return encodedField{code: 12}, nil
	case 8192:
		return encodedField{code: 13}, nil
	case 16384:
		return encodedField{code: 14}, nil
	case 32768:
		return encodedField{code: 15}, nil
	}
	if size >= 1 && size <= 256 {
		return encodedField{code: 6, extra: uint16(size - 1), bits: 8}, nil
	}
	if size <= 65535 {
		return encodedField{code: 7, extra: uint16(size - 1), bits: 16}, nil
	}
	return encodedField{}, fmt.Errorf("invalid FLAC block size: %d", size)
}
func encodeSampleRate(rate int) (encodedField, error) {
	switch rate {
	case 88200:
		return encodedField{code: 1}, nil
	case 176400:
		return encodedField{code: 2}, nil
	case 192000:
		return encodedField{code: 3}, nil
	case 8000:
		return encodedField{code: 4}, nil
	case 16000:
		return encodedField{code: 5}, nil
	case 22050:
		return encodedField{code: 6}, nil
	case 24000:
		return encodedField{code: 7}, nil
	case 32000:
		return encodedField{code: 8}, nil
	case 44100:
		return encodedField{code: 9}, nil
	case 48000:
		return encodedField{code: 10}, nil
	case 96000:
		return encodedField{code: 11}, nil
	}
	if rate > 0 && rate%1000 == 0 && rate/1000 <= 255 {
		return encodedField{code: 12, extra: uint16(rate / 1000), bits: 8}, nil
	}
	if rate > 0 && rate <= 65535 {
		return encodedField{code: 13, extra: uint16(rate), bits: 16}, nil
	}
	if rate > 0 && rate%10 == 0 && rate/10 <= 65535 {
		return encodedField{code: 14, extra: uint16(rate / 10), bits: 16}, nil
	}
	if rate >= 1 && rate <= 1048575 {
		return encodedField{}, nil
	}
	return encodedField{}, fmt.Errorf("invalid FLAC sample rate: %d", rate)
}
func encodeBitsPerSample(depth int) (uint8, error) {
	switch depth {
	case 4, 5, 6, 7, 9, 10, 11, 13, 14, 15, 17, 18, 19, 21, 22, 23, 25, 26, 27, 28, 29, 30, 31:
		return 0, nil
	case 8:
		return 1, nil
	case 12:
		return 2, nil
	case 16:
		return 4, nil
	case 20:
		return 5, nil
	case 24:
		return 6, nil
	case 32:
		return 7, nil
	default:
		return 0, fmt.Errorf("unsupported FLAC bit depth: %d", depth)
	}
}
func encodeUTF8Number(w *bits.Writer, value uint64) error {
	if value <= 0x7f {
		w.Byte(byte(value))
		return nil
	}
	length := 0
	switch {
	case value <= 0x7ff:
		length = 2
	case value <= 0xffff:
		length = 3
	case value <= 0x1fffff:
		length = 4
	case value <= 0x3ffffff:
		length = 5
	case value <= 0x7fffffff:
		length = 6
	case value <= 0xfffffffff:
		length = 7
	default:
		return errors.New("FLAC UTF-8 coded number is too large")
	}
	continuation := make([]byte, length-1)
	for i := len(continuation) - 1; i >= 0; i-- {
		continuation[i] = 0x80 | byte(value&0x3f)
		value >>= 6
	}
	w.Byte(byte(0xff<<(8-length)) | byte(value))
	for _, b := range continuation {
		w.Byte(b)
	}
	return nil
}
