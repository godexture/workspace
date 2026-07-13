package internal

import (
	"errors"
	"fmt"

	"github.com/godexture/sdk/bits"
)

func readFrameHeader(r *bits.Reader, info streamInfo) (frameHeader, error) {
	sync, err := r.ReadBits64(14)
	if err != nil {
		return frameHeader{}, err
	}
	if sync != 0x3ffe {
		return frameHeader{}, errors.New("invalid FLAC frame sync")
	}
	reserved, err := r.ReadBits64(1)
	if err != nil {
		return frameHeader{}, err
	}
	if reserved != 0 {
		return frameHeader{}, errors.New("invalid FLAC reserved frame header bit")
	}
	blockingStrategy, err := r.ReadBits64(1)
	if err != nil {
		return frameHeader{}, err
	}

	blockSizeCode, err := r.ReadBits64(4)
	if err != nil {
		return frameHeader{}, err
	}
	sampleRateCode, err := r.ReadBits64(4)
	if err != nil {
		return frameHeader{}, err
	}
	channelAssignment, err := r.ReadBits64(4)
	if err != nil {
		return frameHeader{}, err
	}
	bitDepthCode, err := r.ReadBits64(3)
	if err != nil {
		return frameHeader{}, err
	}
	reserved, err = r.ReadBits64(1)
	if err != nil {
		return frameHeader{}, err
	}
	if reserved != 0 {
		return frameHeader{}, errors.New("invalid FLAC reserved frame header bit")
	}

	number, err := readUTF8CodedNumber(r)
	if err != nil {
		return frameHeader{}, fmt.Errorf("decode FLAC frame number: %w", err)
	}

	blockSize, err := decodeBlockSize(r, uint8(blockSizeCode), info)
	if err != nil {
		return frameHeader{}, err
	}
	sampleRate, err := decodeSampleRate(r, uint8(sampleRateCode), info)
	if err != nil {
		return frameHeader{}, err
	}
	bitsPerSample, err := decodeBitsPerSample(uint8(bitDepthCode), info)
	if err != nil {
		return frameHeader{}, err
	}
	channels, err := decodeChannelCount(uint8(channelAssignment), info)
	if err != nil {
		return frameHeader{}, err
	}

	headerCRC, err := r.ReadBits64(8)
	if err != nil {
		return frameHeader{}, err
	}

	return frameHeader{
		blockSize:         blockSize,
		sampleRate:        sampleRate,
		channels:          channels,
		channelAssignment: uint8(channelAssignment),
		bitsPerSample:     bitsPerSample,
		blockingStrategy:  blockingStrategy != 0,
		number:            number,
		headerBytes:       r.BytePos(),
		headerCRC:         byte(headerCRC),
	}, nil
}



func readUTF8CodedNumber(r *bits.Reader) (uint64, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if first&0x80 == 0 {
		return uint64(first), nil
	}

	var length int
	mask := byte(0x80)
	for first&mask != 0 {
		length++
		mask >>= 1
	}
	if length < 2 || length > 7 {
		return 0, errors.New("invalid FLAC UTF-8 coded number")
	}

	value := uint64(first & (0xff >> (length + 1)))
	for i := 1; i < length; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b&0xc0 != 0x80 {
			return 0, errors.New("invalid FLAC UTF-8 continuation byte")
		}
		value = (value << 6) | uint64(b&0x3f)
	}
	return value, nil
}

func decodeBlockSize(r *bits.Reader, code uint8, info streamInfo) (int, error) {
	switch code {
	case 0:
		return 0, errors.New("reserved FLAC block size code")
	case 1:
		return 192, nil
	case 2, 3, 4, 5:
		return 576 << (code - 2), nil
	case 6:
		value, err := r.ReadBits64(8)
		return int(value) + 1, err
	case 7:
		value, err := r.ReadBits64(16)
		return int(value) + 1, err
	case 8, 9, 10, 11, 12, 13, 14, 15:
		return 256 << (code - 8), nil
	default:
		return int(info.MaxBlockSize), nil
	}
}

func decodeSampleRate(r *bits.Reader, code uint8, info streamInfo) (int, error) {
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
		value, err := r.ReadBits64(8)
		return int(value) * 1000, err
	case 13:
		value, err := r.ReadBits64(16)
		return int(value), err
	case 14:
		value, err := r.ReadBits64(16)
		return int(value) * 10, err
	default:
		return 0, errors.New("reserved FLAC sample rate code")
	}
}

func decodeBitsPerSample(code uint8, info streamInfo) (int, error) {
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
	case 3:
		return 0, errors.New("reserved FLAC bit depth code")
	case 7:
		return 32, nil
	default:
		return info.BitsPerSample, nil
	}
}

func decodeChannelCount(channelAssignment uint8, info streamInfo) (int, error) {
	switch {
	case channelAssignment <= 7:
		return int(channelAssignment) + 1, nil
	case channelAssignment >= 8 && channelAssignment <= 10:
		return 2, nil
	default:
		return 0, errors.New("reserved FLAC channel assignment")
	}
}
