package frame

import (
	"errors"
	"fmt"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
)

func decodeHeader(r *bits.Reader, info streaminfo.StreamInfo) (Header, error) {
	sync, err := r.ReadBits32(14)
	if err != nil {
		return Header{}, err
	}
	if sync != 0x3ffe {
		return Header{}, errors.New("invalid FLAC frame sync")
	}
	reserved, err := r.ReadBits32(1)
	if err != nil {
		return Header{}, err
	}
	if reserved != 0 {
		return Header{}, errors.New("invalid FLAC reserved frame header bit")
	}
	blocking, err := r.ReadBits32(1)
	if err != nil {
		return Header{}, err
	}
	blockCode, err := r.ReadBits32(4)
	if err != nil {
		return Header{}, err
	}
	rateCode, err := r.ReadBits32(4)
	if err != nil {
		return Header{}, err
	}
	assignment, err := r.ReadBits32(4)
	if err != nil {
		return Header{}, err
	}
	depthCode, err := r.ReadBits32(3)
	if err != nil {
		return Header{}, err
	}
	reserved, err = r.ReadBits32(1)
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
	crc, err := r.ReadBits32(8)
	if err != nil {
		return Header{}, err
	}
	return Header{BlockSize: blockSize, SampleRate: sampleRate, Channels: channels, ChannelAssignment: uint8(assignment), BitsPerSample: bitsPerSample, BlockingStrategy: blocking != 0, Number: number, HeaderBytes: r.BytePos(), HeaderCRC: byte(crc)}, nil
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
		v, e := r.ReadBits32(8)
		return int(v) + 1, e
	case 7:
		v, e := r.ReadBits32(16)
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
		v, e := r.ReadBits32(8)
		return int(v) * 1000, e
	case 13:
		v, e := r.ReadBits32(16)
		return int(v), e
	case 14:
		v, e := r.ReadBits32(16)
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
