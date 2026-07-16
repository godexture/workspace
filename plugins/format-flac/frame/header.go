// Package frame implements the FLAC frame wire format.
package frame

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

type Header struct {
	BlockSize         int
	SampleRate        int
	Channels          int
	ChannelAssignment uint8
	BitsPerSample     int
	BlockingStrategy  bool
	Number            uint64
	HeaderBytes       int
	HeaderCRC         byte
	FrameBytes        int
}

func ParseHeader(data []byte, info streaminfo.StreamInfo) (Header, error) {
	r := bits.New(data)
	header, err := decodeHeader(r, info)
	if err != nil {
		return Header{}, err
	}
	if header.HeaderBytes > len(data) {
		return Header{}, io.ErrUnexpectedEOF
	}
	if header.HeaderBytes < 1 || hash.CRC8(data[:header.HeaderBytes-1]) != header.HeaderCRC {
		return Header{}, errors.New("invalid FLAC frame header CRC-8")
	}
	return header, nil
}

func decodeHeader(r *bits.Reader, info streaminfo.StreamInfo) (Header, error) {
	sync, err := r.ReadBits64(14)
	if err != nil {
		return Header{}, err
	}
	if sync != 0x3ffe {
		return Header{}, errors.New("invalid FLAC frame sync")
	}
	reserved, err := r.ReadBits64(1)
	if err != nil {
		return Header{}, err
	}
	if reserved != 0 {
		return Header{}, errors.New("invalid FLAC reserved frame header bit")
	}
	blocking, err := r.ReadBits64(1)
	if err != nil {
		return Header{}, err
	}
	blockCode, err := r.ReadBits64(4)
	if err != nil {
		return Header{}, err
	}
	rateCode, err := r.ReadBits64(4)
	if err != nil {
		return Header{}, err
	}
	assignment, err := r.ReadBits64(4)
	if err != nil {
		return Header{}, err
	}
	depthCode, err := r.ReadBits64(3)
	if err != nil {
		return Header{}, err
	}
	reserved, err = r.ReadBits64(1)
	if err != nil {
		return Header{}, err
	}
	if reserved != 0 {
		return Header{}, errors.New("invalid FLAC reserved frame header bit")
	}
	number, err := decodeUTF8Number(r)
	if err != nil {
		return Header{}, fmt.Errorf("decode FLAC frame number: %w", err)
	}
	if blocking == 0 && number > 0x7fffffff {
		return Header{}, errors.New("FLAC fixed-blocking frame number exceeds 31 bits")
	}
	blockSize, err := decodeBlockSize(r, uint8(blockCode), info)
	if err != nil {
		return Header{}, err
	}
	sampleRate, err := decodeSampleRate(r, uint8(rateCode), info)
	if err != nil {
		return Header{}, err
	}
	bitsPerSample, err := decodeBitsPerSample(uint8(depthCode), info)
	if err != nil {
		return Header{}, err
	}
	channels, err := decodeChannelCount(uint8(assignment))
	if err != nil {
		return Header{}, err
	}
	crc, err := r.ReadBits64(8)
	if err != nil {
		return Header{}, err
	}
	return Header{BlockSize: blockSize, SampleRate: sampleRate, Channels: channels, ChannelAssignment: uint8(assignment), BitsPerSample: bitsPerSample, BlockingStrategy: blocking != 0, Number: number, HeaderBytes: r.BytePos(), HeaderCRC: byte(crc)}, nil
}

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

func decodeUTF8Number(r *bits.Reader) (uint64, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if first&0x80 == 0 {
		return uint64(first), nil
	}
	length := 0
	for mask := byte(0x80); first&mask != 0; mask >>= 1 {
		length++
	}
	if length < 2 || length > 7 {
		return 0, errors.New("invalid FLAC UTF-8 coded number")
	}
	value := uint64(first & (0xff >> (length + 1)))
	for range length - 1 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b&0xc0 != 0x80 {
			return 0, errors.New("invalid FLAC UTF-8 continuation byte")
		}
		value = value<<6 | uint64(b&0x3f)
	}
	minimum := []uint64{0, 0, 0x80, 0x800, 0x10000, 0x200000, 0x4000000, 0x80000000}[length]
	if value < minimum || value > 0xfffffffff {
		return 0, errors.New("non-canonical or oversized FLAC UTF-8 coded number")
	}
	return value, nil
}

func decodeBlockSize(r *bits.Reader, code uint8, info streaminfo.StreamInfo) (int, error) {
	switch code {
	case 0:
		return 0, errors.New("reserved FLAC block size code")
	case 1:
		return 192, nil
	case 2, 3, 4, 5:
		return 576 << (code - 2), nil
	case 6:
		v, e := r.ReadBits64(8)
		return int(v) + 1, e
	case 7:
		v, e := r.ReadBits64(16)
		if e != nil {
			return 0, e
		}
		if v == 0xffff {
			return 0, errors.New("FLAC block size exceeds 65535")
		}
		return int(v) + 1, nil
	case 8, 9, 10, 11, 12, 13, 14, 15:
		return 256 << (code - 8), nil
	default:
		return int(info.MaxBlockSize), nil
	}
}
func decodeSampleRate(r *bits.Reader, code uint8, info streaminfo.StreamInfo) (int, error) {
	switch code {
	case 0:
		return info.SampleRate, nil
	case 1:
		return 88200, nil
	case 2:
		return 176400, nil
	case 3:
		return 192000, nil
	case 4:
		return 8000, nil
	case 5:
		return 16000, nil
	case 6:
		return 22050, nil
	case 7:
		return 24000, nil
	case 8:
		return 32000, nil
	case 9:
		return 44100, nil
	case 10:
		return 48000, nil
	case 11:
		return 96000, nil
	case 12:
		v, e := r.ReadBits64(8)
		return int(v) * 1000, e
	case 13:
		v, e := r.ReadBits64(16)
		return int(v), e
	case 14:
		v, e := r.ReadBits64(16)
		return int(v) * 10, e
	default:
		return 0, errors.New("reserved FLAC sample rate code")
	}
}
func decodeBitsPerSample(code uint8, info streaminfo.StreamInfo) (int, error) {
	switch code {
	case 0:
		return info.BitsPerSample, nil
	case 1:
		return 8, nil
	case 2:
		return 12, nil
	case 4:
		return 16, nil
	case 5:
		return 20, nil
	case 6:
		return 24, nil
	case 7:
		return 32, nil
	case 3:
		return 0, errors.New("reserved FLAC bit depth code")
	default:
		return info.BitsPerSample, nil
	}
}
func decodeChannelCount(assignment uint8) (int, error) {
	if assignment <= 7 {
		return int(assignment) + 1, nil
	}
	if assignment <= 10 {
		return 2, nil
	}
	return 0, errors.New("reserved FLAC channel assignment")
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
