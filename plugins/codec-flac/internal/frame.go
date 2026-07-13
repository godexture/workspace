package internal

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
)

func decodeFLACFrame(data []byte, info streamInfo) (decodedFrame, error) {
	reader := newBitReader(data)
	header, err := readFrameHeader(reader, info)
	if err != nil {
		return decodedFrame{}, err
	}

	samples := make([][]int32, header.channels)
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

	reader.alignByte()
	if _, err := reader.readBits(16); err != nil {
		return decodedFrame{}, err
	}

	return decodedFrame{
		header:  header,
		samples: samples,
		bytes:   reader.bytePos(),
	}, nil
}

func readFrameHeader(r *bitReader, info streamInfo) (frameHeader, error) {
	sync, err := r.readBits(14)
	if err != nil {
		return frameHeader{}, err
	}
	if sync != 0x3ffe {
		return frameHeader{}, errors.New("invalid FLAC frame sync")
	}
	reserved, err := r.readBits(1)
	if err != nil {
		return frameHeader{}, err
	}
	if reserved != 0 {
		return frameHeader{}, errors.New("invalid FLAC reserved frame header bit")
	}
	if _, err := r.readBits(1); err != nil { // blocking strategy
		return frameHeader{}, err
	}

	blockSizeCode, err := r.readBits(4)
	if err != nil {
		return frameHeader{}, err
	}
	sampleRateCode, err := r.readBits(4)
	if err != nil {
		return frameHeader{}, err
	}
	channelAssignment, err := r.readBits(4)
	if err != nil {
		return frameHeader{}, err
	}
	bitDepthCode, err := r.readBits(3)
	if err != nil {
		return frameHeader{}, err
	}
	reserved, err = r.readBits(1)
	if err != nil {
		return frameHeader{}, err
	}
	if reserved != 0 {
		return frameHeader{}, errors.New("invalid FLAC reserved frame header bit")
	}

	if _, err := readUTF8CodedNumber(r); err != nil {
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

	if _, err := r.readBits(8); err != nil { // CRC-8, currently not validated
		return frameHeader{}, err
	}

	return frameHeader{
		blockSize:         blockSize,
		sampleRate:        sampleRate,
		channels:          channels,
		channelAssignment: uint8(channelAssignment),
		bitsPerSample:     bitsPerSample,
	}, nil
}

func readUTF8CodedNumber(r *bitReader) (uint64, error) {
	first, err := r.readByteAligned()
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
		b, err := r.readByteAligned()
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

func decodeBlockSize(r *bitReader, code uint8, info streamInfo) (int, error) {
	switch code {
	case 0:
		return 0, errors.New("reserved FLAC block size code")
	case 1:
		return 192, nil
	case 2, 3, 4, 5:
		return 576 << (code - 2), nil
	case 6:
		value, err := r.readBits(8)
		return int(value) + 1, err
	case 7:
		value, err := r.readBits(16)
		return int(value) + 1, err
	case 8, 9, 10, 11, 12, 13, 14, 15:
		return 256 << (code - 8), nil
	default:
		return int(info.MaxBlockSize), nil
	}
}

func decodeSampleRate(r *bitReader, code uint8, info streamInfo) (int, error) {
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
		value, err := r.readBits(8)
		return int(value) * 1000, err
	case 13:
		value, err := r.readBits(16)
		return int(value), err
	case 14:
		value, err := r.readBits(16)
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
	case 3, 7:
		return 0, errors.New("reserved FLAC bit depth code")
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

func readSubframe(r *bitReader, blockSize, bitsPerSample int) ([]int32, error) {
	zero, err := r.readBits(1)
	if err != nil {
		return nil, err
	}
	if zero != 0 {
		return nil, errors.New("invalid FLAC subframe header")
	}
	typeCode, err := r.readBits(6)
	if err != nil {
		return nil, err
	}
	wastedFlag, err := r.readBits(1)
	if err != nil {
		return nil, err
	}

	wastedBits := uint64(0)
	if wastedFlag != 0 {
		wastedBits, err = r.readUnary()
		if err != nil {
			return nil, err
		}
		wastedBits++
		if int(wastedBits) >= bitsPerSample {
			return nil, errors.New("invalid FLAC wasted-bits count")
		}
		bitsPerSample -= int(wastedBits)
	}

	samples := make([]int32, blockSize)
	switch {
	case typeCode == 0:
		value, err := r.readSigned(uint8(bitsPerSample))
		if err != nil {
			return nil, err
		}
		for i := range samples {
			samples[i] = value
		}

	case typeCode == 1:
		for i := range samples {
			value, err := r.readSigned(uint8(bitsPerSample))
			if err != nil {
				return nil, err
			}
			samples[i] = value
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
		precisionRaw, err := r.readBits(4)
		if err != nil {
			return nil, err
		}
		if precisionRaw == 15 {
			return nil, errors.New("invalid FLAC LPC coefficient precision")
		}
		precision := int(precisionRaw) + 1
		shiftRaw, err := r.readBits(5)
		if err != nil {
			return nil, err
		}
		shift := signExtend(shiftRaw, 5)
		coefficients := make([]int32, order)
		for i := range coefficients {
			coeff, err := r.readSigned(uint8(precision))
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
			samples[i] = int32(sum) + residual[i-order]
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

func readWarmupSamples(r *bitReader, samples []int32, order, bitsPerSample int) error {
	if order > len(samples) {
		return errors.New("FLAC predictor order exceeds block size")
	}
	for i := 0; i < order; i++ {
		value, err := r.readSigned(uint8(bitsPerSample))
		if err != nil {
			return err
		}
		samples[i] = value
	}
	return nil
}

func readResidual(r *bitReader, blockSize, predictorOrder int) ([]int32, error) {
	method, err := r.readBits(2)
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
	partitionOrderRaw, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	partitionOrder := int(partitionOrderRaw)
	partitions := 1 << partitionOrder
	if blockSize%partitions != 0 {
		return nil, errors.New("FLAC residual partition order does not divide block size")
	}

	residualCount := blockSize - predictorOrder
	residual := make([]int32, 0, residualCount)
	partitionSamples := blockSize / partitions
	for partition := 0; partition < partitions; partition++ {
		samplesInPartition := partitionSamples
		if partition == 0 {
			samplesInPartition -= predictorOrder
		}
		if samplesInPartition < 0 {
			return nil, errors.New("FLAC residual partition smaller than predictor order")
		}

		param, err := r.readBits(paramBits)
		if err != nil {
			return nil, err
		}
		if param == escape {
			rawBits, err := r.readBits(5)
			if err != nil {
				return nil, err
			}
			for i := 0; i < samplesInPartition; i++ {
				value, err := r.readSigned(uint8(rawBits))
				if err != nil {
					return nil, err
				}
				residual = append(residual, value)
			}
			continue
		}

		for i := 0; i < samplesInPartition; i++ {
			value, err := readRiceSigned(r, uint8(param))
			if err != nil {
				return nil, err
			}
			residual = append(residual, value)
		}
	}
	if len(residual) != residualCount {
		return nil, errors.New("decoded FLAC residual size mismatch")
	}
	return residual, nil
}

func readRiceSigned(r *bitReader, param uint8) (int32, error) {
	quotient, err := r.readUnary()
	if err != nil {
		return 0, err
	}
	remainder, err := r.readBits(param)
	if err != nil {
		return 0, err
	}
	unsigned := (quotient << param) | remainder
	if unsigned&1 == 0 {
		return int32(unsigned >> 1), nil
	}
	return -int32((unsigned >> 1) + 1), nil
}

func fixedPrediction(samples []int32, index, order int) int32 {
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

func decorrelate(samples [][]int32, assignment uint8) {
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
	frame := media.NewAudioFrame(format, layout, decoded.header.sampleRate, decoded.header.blockSize)
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
