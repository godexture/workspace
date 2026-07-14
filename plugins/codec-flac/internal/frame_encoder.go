package internal

import (
	"encoding/binary"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func encodeFrame(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, maxFixedOrder int) ([]byte, error) {
	return encodeFrameWithOptions(samples, sampleRate, bitsPerSample, frameNumber, frameOptions{
		maxFixedOrder: maxFixedOrder, maxLPCOrder: 32, maxRicePartitionOrder: 8,
		enableWastedBits: true, enableStereoDecorrelation: true, streamableSubset: true,
	})
}

type frameOptions struct {
	maxFixedOrder, maxLPCOrder, maxRicePartitionOrder                               int
	enableWastedBits, enableStereoDecorrelation, streamableSubset, variableBlocking bool
}

func encodeFrameWithOptions(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options frameOptions) ([]byte, error) {
	if bitsPerSample < 4 || bitsPerSample > 32 {
		return nil, fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	if sampleRate < 1 || sampleRate > 1048575 {
		return nil, fmt.Errorf("invalid FLAC sample rate: %d", sampleRate)
	}
	if options.maxRicePartitionOrder < 0 || options.maxRicePartitionOrder > 15 || options.streamableSubset && options.maxRicePartitionOrder > 8 {
		return nil, fmt.Errorf("invalid FLAC Rice partition order: %d", options.maxRicePartitionOrder)
	}
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
	if options.streamableSubset {
		if maxBlockSize := streamableMaxBlockSize(sampleRate); blockSize > maxBlockSize {
			return nil, fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", blockSize, maxBlockSize, sampleRate)
		}
		// RFC 9639 Section 7: subset streams at <= 48 kHz MUST NOT use LPC
		// orders above 12. maxLPCOrder is an upper bound, so cap rather than
		// reject.
		if sampleRate <= 48000 && options.maxLPCOrder > streamableMaxLPCOrder {
			options.maxLPCOrder = streamableMaxLPCOrder
		}
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
	assignment, channels, candidates, err := chooseChannelAssignment(samples, bitsPerSample, options)
	if err != nil {
		return nil, err
	}
	if err := writeFrameHeaderWithAssignmentOptions(w, blockSize, sampleRate, assignmentForChannels(assignment, len(samples)), bitsPerSample, frameNumber, options.variableBlocking, options.streamableSubset); err != nil {
		return nil, err
	}
	for ch := range channels {
		channelBits := bitsPerSample
		if assignment == 8 && ch == 1 || assignment == 9 && ch == 0 || assignment == 10 && ch == 1 {
			channelBits++
		}
		if err := writeSubframeCandidate(w, channels[ch], channelBits, candidates[ch]); err != nil {
			return nil, fmt.Errorf("encode FLAC subframe %d: %w", ch, err)
		}
	}
	w.PadToByte()
	frame := append([]byte(nil), w.Bytes()...)
	crc := hash.CRC16(frame)
	frame = append(frame, byte(crc>>8), byte(crc))
	return frame, nil
}

func assignmentForChannels(assignment uint8, channels int) uint8 {
	if assignment != 0 {
		return assignment
	}
	return uint8(channels - 1)
}

// streamableMaxLPCOrder is the largest LPC order the streamable subset
// permits for sample rates at or below 48 kHz (RFC 9639 Section 7).
const streamableMaxLPCOrder = 12

func streamableMaxBlockSize(sampleRate int) int {
	if sampleRate <= 48000 {
		return 4608
	}
	return 16384
}

// chooseChannelAssignment picks the cheapest channel assignment and returns
// the channels to encode together with their already-searched subframe
// candidates, so the caller writes exactly what was costed instead of
// repeating the search. For stereo, the four decorrelation modes reuse the
// four unique channel searches (left, right, mid, side).
func chooseChannelAssignment(samples [][]int64, bitsPerSample int, options frameOptions) (uint8, [][]int64, []subframeCandidate, error) {
	if !options.enableStereoDecorrelation || len(samples) != 2 {
		candidates := make([]subframeCandidate, len(samples))
		for ch := range samples {
			candidates[ch] = bestSubframe(samples[ch], bitsPerSample, options)
			if !candidates[ch].valid {
				return 0, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
			}
		}
		return 0, samples, candidates, nil
	}

	left, right := samples[0], samples[1]
	mid := make([]int64, len(left))
	side := make([]int64, len(left))
	for i := range left {
		mid[i] = (left[i] + right[i]) >> 1
		side[i] = left[i] - right[i]
	}
	leftCandidate := bestSubframe(left, bitsPerSample, options)
	rightCandidate := bestSubframe(right, bitsPerSample, options)
	midCandidate := bestSubframe(mid, bitsPerSample, options)
	sideCandidate := bestSubframe(side, bitsPerSample+1, options)

	assignments := []struct {
		assignment uint8
		channels   [][]int64
		candidates []subframeCandidate
	}{
		{0, [][]int64{left, right}, []subframeCandidate{leftCandidate, rightCandidate}},
		{8, [][]int64{left, side}, []subframeCandidate{leftCandidate, sideCandidate}},
		{9, [][]int64{side, right}, []subframeCandidate{sideCandidate, rightCandidate}},
		{10, [][]int64{mid, side}, []subframeCandidate{midCandidate, sideCandidate}},
	}
	best := uint64(^uint64(0))
	bestIndex := -1
	for i, option := range assignments {
		if !option.candidates[0].valid || !option.candidates[1].valid {
			continue
		}
		cost := option.candidates[0].costBits + option.candidates[1].costBits
		if cost < best {
			best, bestIndex = cost, i
		}
	}
	if bestIndex < 0 {
		return 0, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
	}
	chosen := assignments[bestIndex]
	return chosen.assignment, chosen.channels, chosen.candidates, nil
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
	if format == media.SampleFormatS16 && (bitsPerSample < 4 || bitsPerSample > 16) {
		return nil, 0, 0, fmt.Errorf("S16 FLAC input requires 4..16 bits per sample, got %d", bitsPerSample)
	}
	if format == media.SampleFormatS32 && (bitsPerSample < 17 || bitsPerSample > 32) {
		return nil, 0, 0, fmt.Errorf("S32 FLAC input requires 17..32 bits per sample, got %d", bitsPerSample)
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
