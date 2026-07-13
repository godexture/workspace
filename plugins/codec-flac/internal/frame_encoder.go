package internal

import (
	"encoding/binary"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func encodeFLACFrame(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, maxFixedOrder int) ([]byte, error) {
	if len(samples) == 0 || len(samples) > 8 {
		return nil, fmt.Errorf("unsupported FLAC channel count: %d", len(samples))
	}
	blockSize := len(samples[0])
	if blockSize == 0 {
		return nil, fmt.Errorf("FLAC frame has no samples")
	}
	if blockSize > 65535 {
		return nil, fmt.Errorf("FLAC frame block size exceeds 65535: %d", blockSize)
	}
	if maxBlockSize := streamableMaxBlockSize(sampleRate); blockSize > maxBlockSize {
		return nil, fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", blockSize, maxBlockSize, sampleRate)
	}
	for ch := range samples {
		if len(samples[ch]) != blockSize {
			return nil, fmt.Errorf("FLAC channel %d has mismatched block size", ch)
		}
		if err := validateSampleRange(samples[ch], bitsPerSample); err != nil {
			return nil, err
		}
	}

	w := bits.NewWriter()
	if err := writeFrameHeader(w, blockSize, sampleRate, len(samples), bitsPerSample, frameNumber); err != nil {
		return nil, err
	}
	for ch := range samples {
		if err := writeBestSubframe(w, samples[ch], bitsPerSample, maxFixedOrder); err != nil {
			return nil, fmt.Errorf("encode FLAC subframe %d: %w", ch, err)
		}
	}
	w.PadToByte()
	frame := append([]byte(nil), w.Bytes()...)
	crc := hash.CRC16(frame)
	frame = append(frame, byte(crc>>8), byte(crc))
	return frame, nil
}

func streamableMaxBlockSize(sampleRate int) int {
	if sampleRate <= 48000 {
		return 4608
	}
	return 16384
}

func audioFrameToSamples(frame *media.AudioFrame, bitsPerSample int) ([][]int64, int, int, error) {
	if frame == nil {
		return nil, 0, 0, fmt.Errorf("FLAC encoder received nil audio frame")
	}
	if frame.Format.IsPlanar() {
		return nil, 0, 0, fmt.Errorf("FLAC encoder does not support planar input format: %s", frame.Format)
	}
	format := frame.Format.Packed()
	if bitsPerSample <= 0 {
		bitsPerSample = frame.BitsPerSample
	}
	if bitsPerSample <= 0 {
		bitsPerSample = bitDepthFromSampleFormat(format)
	}
	if format == media.SampleFormatS16 && bitsPerSample != 16 {
		return nil, 0, 0, fmt.Errorf("S16 FLAC input requires 16 bits per sample, got %d", bitsPerSample)
	}
	if format == media.SampleFormatS32 && bitsPerSample != 24 && bitsPerSample != 32 {
		return nil, 0, 0, fmt.Errorf("S32 FLAC input requires 24 or 32 bits per sample, got %d", bitsPerSample)
	}
	if format != media.SampleFormatS16 && format != media.SampleFormatS32 {
		return nil, 0, 0, fmt.Errorf("unsupported FLAC input format: %s", frame.Format)
	}

	channels := frame.Layout.ChannelCount()
	if channels < 1 || channels > 8 {
		return nil, 0, 0, fmt.Errorf("unsupported FLAC channel count: %d", channels)
	}
	if frame.SampleRate <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid FLAC sample rate: %d", frame.SampleRate)
	}
	if frame.Samples <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid FLAC sample count: %d", frame.Samples)
	}

	planes := frame.Planes()
	if len(planes) == 0 {
		return nil, 0, 0, fmt.Errorf("FLAC input has no audio plane")
	}
	plane := planes[0]
	bytesPerSample := format.BytesPerSample()
	wantBytes := frame.Samples * channels * bytesPerSample
	if len(plane) < wantBytes {
		return nil, 0, 0, fmt.Errorf("FLAC input plane is too short: got %d, want %d", len(plane), wantBytes)
	}

	samples := make([][]int64, channels)
	for ch := range samples {
		samples[ch] = make([]int64, frame.Samples)
	}
	for i := 0; i < frame.Samples; i++ {
		for ch := 0; ch < channels; ch++ {
			offset := (i*channels + ch) * bytesPerSample
			var value int64
			switch format {
			case media.SampleFormatS16:
				value = int64(int16(binary.LittleEndian.Uint16(plane[offset : offset+2])))
			case media.SampleFormatS32:
				value = int64(int32(binary.LittleEndian.Uint32(plane[offset : offset+4])))
			}
			samples[ch][i] = value
		}
	}
	if err := validateSampleRange(flattenForValidation(samples), bitsPerSample); err != nil {
		return nil, 0, 0, err
	}
	return samples, frame.SampleRate, bitsPerSample, nil
}

func validateSampleRange(samples []int64, bitsPerSample int) error {
	if bitsPerSample <= 0 || bitsPerSample > 32 {
		return fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	min := -(int64(1) << uint(bitsPerSample-1))
	max := (int64(1) << uint(bitsPerSample-1)) - 1
	for _, sample := range samples {
		if sample < min || sample > max {
			return fmt.Errorf("FLAC sample %d outside %d-bit range", sample, bitsPerSample)
		}
	}
	return nil
}

func flattenForValidation(samples [][]int64) []int64 {
	var out []int64
	for _, channel := range samples {
		out = append(out, channel...)
	}
	return out
}

func cloneSampleBlock(samples [][]int64, start, end int) [][]int64 {
	block := make([][]int64, len(samples))
	for ch := range samples {
		block[ch] = append([]int64(nil), samples[ch][start:end]...)
	}
	return block
}
