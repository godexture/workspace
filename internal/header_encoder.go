package internal

import (
	"errors"
	"fmt"

	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

type encodedBlockSize struct {
	code  uint8
	extra uint16
	bits  uint8
}

type encodedSampleRate struct {
	code  uint8
	extra uint16
	bits  uint8
}

func writeFrameHeader(w *bits.Writer, blockSize, sampleRate, channels, bitsPerSample int, frameNumber uint64) error {
	return writeFrameHeaderWithStrategy(w, blockSize, sampleRate, channels, bitsPerSample, frameNumber, false)
}

func writeFrameHeaderWithStrategy(w *bits.Writer, blockSize, sampleRate, channels, bitsPerSample int, frameNumber uint64, variable bool) error {
	channelAssignment, err := encodeChannelAssignment(channels)
	if err != nil {
		return err
	}
	return writeFrameHeaderWithAssignment(w, blockSize, sampleRate, channelAssignment, bitsPerSample, frameNumber, variable)
}

func writeFrameHeaderWithAssignment(w *bits.Writer, blockSize, sampleRate int, channelAssignment uint8, bitsPerSample int, frameNumber uint64, variable bool) error {
	return writeFrameHeaderWithAssignmentOptions(w, blockSize, sampleRate, channelAssignment, bitsPerSample, frameNumber, variable, false)
}

func writeFrameHeaderWithAssignmentOptions(w *bits.Writer, blockSize, sampleRate int, channelAssignment uint8, bitsPerSample int, frameNumber uint64, variable, streamableSubset bool) error {
	blockSizeCode, err := encodeBlockSizeCode(blockSize)
	if err != nil {
		return err
	}
	sampleRateCode, err := encodeSampleRateCode(sampleRate)
	if err != nil {
		return err
	}
	bitDepthCode, err := encodeBitsPerSampleCode(bitsPerSample)
	if err != nil {
		return err
	}
	if channelAssignment > 10 {
		return fmt.Errorf("invalid FLAC channel assignment: %d", channelAssignment)
	}
	if streamableSubset && (sampleRateCode.code == 0 || bitDepthCode == 0) {
		return errors.New("FLAC streamable subset requires explicit frame sample-rate and bit-depth codes")
	}

	w.Bits64(0x3ffe, 14) // sync
	w.Bits64(0, 1)       // reserved
	if variable {
		w.Bits64(1, 1)
	} else {
		w.Bits64(0, 1)
	}
	w.Bits64(uint64(blockSizeCode.code), 4)
	w.Bits64(uint64(sampleRateCode.code), 4)
	w.Bits64(uint64(channelAssignment), 4)
	w.Bits64(uint64(bitDepthCode), 3)
	w.Bits64(0, 1) // reserved
	if err := writeUTF8CodedNumber(w, frameNumber); err != nil {
		return err
	}
	if blockSizeCode.bits > 0 {
		w.Bits64(uint64(blockSizeCode.extra), blockSizeCode.bits)
	}
	if sampleRateCode.bits > 0 {
		w.Bits64(uint64(sampleRateCode.extra), sampleRateCode.bits)
	}
	w.Byte(hash.CRC8(w.Bytes()))
	return nil
}

func encodeBlockSizeCode(blockSize int) (encodedBlockSize, error) {
	switch blockSize {
	case 192:
		return encodedBlockSize{code: 1}, nil
	case 576:
		return encodedBlockSize{code: 2}, nil
	case 1152:
		return encodedBlockSize{code: 3}, nil
	case 2304:
		return encodedBlockSize{code: 4}, nil
	case 4608:
		return encodedBlockSize{code: 5}, nil
	case 256:
		return encodedBlockSize{code: 8}, nil
	case 512:
		return encodedBlockSize{code: 9}, nil
	case 1024:
		return encodedBlockSize{code: 10}, nil
	case 2048:
		return encodedBlockSize{code: 11}, nil
	case 4096:
		return encodedBlockSize{code: 12}, nil
	case 8192:
		return encodedBlockSize{code: 13}, nil
	case 16384:
		return encodedBlockSize{code: 14}, nil
	case 32768:
		return encodedBlockSize{code: 15}, nil
	}
	if blockSize >= 1 && blockSize <= 256 {
		return encodedBlockSize{code: 6, extra: uint16(blockSize - 1), bits: 8}, nil
	}
	if blockSize >= 1 && blockSize <= 65535 {
		return encodedBlockSize{code: 7, extra: uint16(blockSize - 1), bits: 16}, nil
	}
	return encodedBlockSize{}, fmt.Errorf("invalid FLAC block size: %d", blockSize)
}

func encodeSampleRateCode(sampleRate int) (encodedSampleRate, error) {
	switch sampleRate {
	case 88200:
		return encodedSampleRate{code: 1}, nil
	case 176400:
		return encodedSampleRate{code: 2}, nil
	case 192000:
		return encodedSampleRate{code: 3}, nil
	case 8000:
		return encodedSampleRate{code: 4}, nil
	case 16000:
		return encodedSampleRate{code: 5}, nil
	case 22050:
		return encodedSampleRate{code: 6}, nil
	case 24000:
		return encodedSampleRate{code: 7}, nil
	case 32000:
		return encodedSampleRate{code: 8}, nil
	case 44100:
		return encodedSampleRate{code: 9}, nil
	case 48000:
		return encodedSampleRate{code: 10}, nil
	case 96000:
		return encodedSampleRate{code: 11}, nil
	}
	if sampleRate > 0 && sampleRate%1000 == 0 && sampleRate/1000 <= 255 {
		return encodedSampleRate{code: 12, extra: uint16(sampleRate / 1000), bits: 8}, nil
	}
	if sampleRate > 0 && sampleRate <= 65535 {
		return encodedSampleRate{code: 13, extra: uint16(sampleRate), bits: 16}, nil
	}
	if sampleRate > 0 && sampleRate%10 == 0 && sampleRate/10 <= 65535 {
		return encodedSampleRate{code: 14, extra: uint16(sampleRate / 10), bits: 16}, nil
	}
	if sampleRate >= 1 && sampleRate <= 1048575 {
		// Code 0 means that STREAMINFO carries the rate. This is the only
		// representation available for arbitrary native-FLAC rates.
		return encodedSampleRate{code: 0}, nil
	}
	return encodedSampleRate{}, fmt.Errorf("invalid FLAC sample rate: %d", sampleRate)
}

func encodeBitsPerSampleCode(bitsPerSample int) (uint8, error) {
	switch bitsPerSample {
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
		return 0, fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
}

func encodeChannelAssignment(channels int) (uint8, error) {
	if channels < 1 || channels > 8 {
		return 0, fmt.Errorf("unsupported FLAC channel count: %d", channels)
	}
	return uint8(channels - 1), nil
}

func writeUTF8CodedNumber(w *bits.Writer, value uint64) error {
	if value <= 0x7f {
		w.Byte(byte(value))
		return nil
	}

	var length int
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
	firstMask := byte(0xff << (8 - length))
	first := firstMask | byte(value)
	w.Byte(first)
	for _, b := range continuation {
		w.Byte(b)
	}
	return nil
}
