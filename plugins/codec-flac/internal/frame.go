package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
)

func decodeFLACFrame(data []byte, info streamInfo) (decodedFrame, error) {
	reader := bits.New(data)
	header, err := readFrameHeader(reader, info)
	if err != nil {
		return decodedFrame{}, err
	}
	if header.headerBytes < 1 || header.headerBytes > len(data) || crc8(data[:header.headerBytes-1]) != header.headerCRC {
		return decodedFrame{}, errors.New("invalid FLAC frame header CRC-8")
	}

	samples := make([][]int64, header.channels)
	for ch := 0; ch < header.channels; ch++ {
		bitsPerSample := header.bitsPerSample
		switch header.channelAssignment {
		case 8:
			if ch == 1 {
				bitsPerSample++
			}
		case 9:
			if ch == 0 {
				bitsPerSample++
			}
		case 10:
			if ch == 1 {
				bitsPerSample++
			}
		}

		channelSamples, err := readSubframe(reader, header.blockSize, bitsPerSample)
		if err != nil {
			return decodedFrame{}, fmt.Errorf("decode FLAC subframe %d: %w", ch, err)
		}
		samples[ch] = channelSamples
	}

	decorrelate(samples, header.channelAssignment)

	reader.SkipToByte()
	footerStart := reader.BytePos()
	footer, err := reader.ReadBits64(16)
	if err != nil {
		return decodedFrame{}, err
	}
	if footerStart > len(data) || crc16(data[:footerStart]) != uint16(footer) {
		return decodedFrame{}, errors.New("invalid FLAC frame footer CRC-16")
	}

	if reader.Overrun() {
		return decodedFrame{}, io.ErrUnexpectedEOF
	}

	header.frameBytes = reader.BytePos()
	return decodedFrame{
		header:  header,
		samples: samples,
		bytes:   reader.BytePos(),
	}, nil
}

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

func crc8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func crc16(data []byte) uint16 {
	var crc uint16
	for _, value := range data {
		crc ^= uint16(value) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
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

func readSubframe(r *bits.Reader, blockSize, bitsPerSample int) ([]int64, error) {
	zero, err := r.ReadBits64(1)
	if err != nil {
		return nil, err
	}
	if zero != 0 {
		return nil, errors.New("invalid FLAC subframe header")
	}
	typeCode, err := r.ReadBits64(6)
	if err != nil {
		return nil, err
	}
	wastedFlag, err := r.ReadBits64(1)
	if err != nil {
		return nil, err
	}

	wastedBits := uint64(0)
	if wastedFlag != 0 {
		wastedBits, err = r.ReadUnary64()
		if err != nil {
			return nil, err
		}
		wastedBits++
		if int(wastedBits) >= bitsPerSample {
			return nil, errors.New("invalid FLAC wasted-bits count")
		}
		bitsPerSample -= int(wastedBits)
	}

	samples := make([]int64, blockSize)
	switch {
	case typeCode == 0:
		value, err := r.ReadSigned64(uint8(bitsPerSample))
		if err != nil {
			return nil, err
		}
		for i := range samples {
			samples[i] = value
		}

	case typeCode == 1:
		for i := range samples {
			samples[i] = r.Signed64(uint8(bitsPerSample))
		}

	case typeCode >= 8 && typeCode <= 12:
		order := int(typeCode - 8)
		if err := readWarmupSamples(r, samples, order, bitsPerSample); err != nil {
			return nil, err
		}
		residual, err := readResidual(r, blockSize, order)
		if err != nil {
			return nil, err
		}
		for i := order; i < blockSize; i++ {
			samples[i] = fixedPrediction(samples, i, order) + residual[i-order]
		}

	case typeCode >= 32 && typeCode <= 63:
		order := int(typeCode - 31)
		if order > blockSize {
			return nil, errors.New("FLAC LPC order exceeds block size")
		}
		if err := readWarmupSamples(r, samples, order, bitsPerSample); err != nil {
			return nil, err
		}
		precisionRaw, err := r.ReadBits64(4)
		if err != nil {
			return nil, err
		}
		if precisionRaw == 15 {
			return nil, errors.New("invalid FLAC LPC coefficient precision")
		}
		precision := int(precisionRaw) + 1
		shiftRaw, err := r.ReadBits64(5)
		if err != nil {
			return nil, err
		}
		shift := signExtend(shiftRaw, 5)
		coefficients := make([]int64, order)
		for i := range coefficients {
			coeff, err := r.ReadSigned64(uint8(precision))
			if err != nil {
				return nil, err
			}
			coefficients[i] = coeff
		}
		residual, err := readResidual(r, blockSize, order)
		if err != nil {
			return nil, err
		}
		for i := order; i < blockSize; i++ {
			var sum int64
			for j := 0; j < order; j++ {
				sum += int64(coefficients[j]) * int64(samples[i-j-1])
			}
			if shift >= 0 {
				sum >>= shift
			} else {
				sum <<= -shift
			}
			samples[i] = sum + residual[i-order]
		}

	default:
		return nil, fmt.Errorf("unsupported FLAC subframe type: %d", typeCode)
	}

	if wastedBits > 0 {
		for i := range samples {
			samples[i] <<= wastedBits
		}
	}
	return samples, nil
}

func readWarmupSamples(r *bits.Reader, samples []int64, order, bitsPerSample int) error {
	if order > len(samples) {
		return errors.New("FLAC predictor order exceeds block size")
	}
	for i := 0; i < order; i++ {
		value, err := r.ReadSigned64(uint8(bitsPerSample))
		if err != nil {
			return err
		}
		samples[i] = value
	}
	return nil
}

// readResidual reads a FLAC residual coding block. Structural validity
// checks here (reserved coding method, partition math, decoded size) are
// FLAC-spec requirements and always return an error. The per-sample reads
// inside each partition are the hot path, so they use the Fast tier (no
// per-call error); a truncated stream in that inner loop surfaces later via
// Reader.Overrun() rather than aborting this function early.
func readResidual(r *bits.Reader, blockSize, predictorOrder int) ([]int64, error) {
	method, err := r.ReadBits64(2)
	if err != nil {
		return nil, err
	}
	var paramBits uint8
	var escape uint64
	switch method {
	case 0:
		paramBits = 4
		escape = 15
	case 1:
		paramBits = 5
		escape = 31
	default:
		return nil, errors.New("reserved FLAC residual coding method")
	}
	partitionOrderRaw, err := r.ReadBits64(4)
	if err != nil {
		return nil, err
	}
	partitionOrder := int(partitionOrderRaw)
	partitions := 1 << partitionOrder
	if blockSize%partitions != 0 {
		return nil, errors.New("FLAC residual partition order does not divide block size")
	}

	residualCount := blockSize - predictorOrder
	residual := make([]int64, 0, residualCount)
	partitionSamples := blockSize / partitions
	for partition := 0; partition < partitions; partition++ {
		samplesInPartition := partitionSamples
		if partition == 0 {
			samplesInPartition -= predictorOrder
		}
		if samplesInPartition < 0 {
			return nil, errors.New("FLAC residual partition smaller than predictor order")
		}

		param, err := r.ReadBits64(paramBits)
		if err != nil {
			return nil, err
		}
		if param == escape {
			rawBits, err := r.ReadBits64(5)
			if err != nil {
				return nil, err
			}
			for i := 0; i < samplesInPartition; i++ {
				residual = append(residual, r.Signed64(uint8(rawBits)))
			}
			continue
		}

		for i := 0; i < samplesInPartition; i++ {
			residual = append(residual, readRiceSigned(r, uint8(param)))
		}
	}
	if len(residual) != residualCount {
		return nil, errors.New("decoded FLAC residual size mismatch")
	}
	return residual, nil
}

// readRiceSigned decodes one Rice-coded residual sample. It is called per
// sample (potentially thousands of times per frame), so it uses the Fast
// tier: a truncated stream here is detected in aggregate via Overrun()
// rather than per call.
func readRiceSigned(r *bits.Reader, param uint8) int64 {
	quotient := r.Unary64()
	remainder := r.Bits64(param)
	unsigned := (quotient << param) | remainder
	if unsigned&1 == 0 {
		return int64(unsigned >> 1)
	}
	return -int64((unsigned >> 1) + 1)
}

func fixedPrediction(samples []int64, index, order int) int64 {
	switch order {
	case 0:
		return 0
	case 1:
		return samples[index-1]
	case 2:
		return 2*samples[index-1] - samples[index-2]
	case 3:
		return 3*samples[index-1] - 3*samples[index-2] + samples[index-3]
	case 4:
		return 4*samples[index-1] - 6*samples[index-2] + 4*samples[index-3] - samples[index-4]
	default:
		return 0
	}
}

func decorrelate(samples [][]int64, assignment uint8) {
	if len(samples) != 2 {
		return
	}
	switch assignment {
	case 8: // left + side
		for i := range samples[0] {
			samples[1][i] = samples[0][i] - samples[1][i]
		}
	case 9: // side + right
		for i := range samples[0] {
			samples[0][i] += samples[1][i]
		}
	case 10: // mid + side
		for i := range samples[0] {
			mid := samples[0][i]<<1 | (samples[1][i] & 1)
			side := samples[1][i]
			samples[0][i] = (mid + side) >> 1
			samples[1][i] = (mid - side) >> 1
		}
	}
}

func buildAudioFrame(decoded decodedFrame) (*media.AudioFrame, error) {
	format := streaminfo.SampleFormat(decoded.header.bitsPerSample)
	layout := streaminfo.ChannelLayout(decoded.header.channels)
	frame := media.NewAudioFrame(
		format,
		layout,
		decoded.header.sampleRate,
		decoded.header.blockSize,
		media.WithAudioBitsPerSample(decoded.header.bitsPerSample),
	)
	plane := frame.Planes()[0]

	bytesPerSample := format.BytesPerSample()
	for sampleIndex := 0; sampleIndex < decoded.header.blockSize; sampleIndex++ {
		for channel := 0; channel < decoded.header.channels; channel++ {
			offset := (sampleIndex*decoded.header.channels + channel) * bytesPerSample
			value := decoded.samples[channel][sampleIndex]
			switch format {
			case media.SampleFormatS16:
				binary.LittleEndian.PutUint16(plane[offset:offset+2], uint16(int16(value)))
			case media.SampleFormatS32:
				binary.LittleEndian.PutUint32(plane[offset:offset+4], uint32(value))
			default:
				return nil, fmt.Errorf("unsupported FLAC output format: %s", format)
			}
		}
	}
	return frame, nil
}

func signExtend(value uint64, bits uint8) int {
	if bits == 0 {
		return 0
	}
	if value&(uint64(1)<<(bits-1)) != 0 {
		value |= ^uint64(0) << bits
	}
	return int(int64(value))
}
