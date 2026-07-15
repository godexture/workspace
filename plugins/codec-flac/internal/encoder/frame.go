package encoder

import (
	"fmt"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func EncodeFrame(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options flac.EncoderConfig) ([]byte, error) {
	var writer bits.Writer
	return encodeFrameWithWriter(samples, sampleRate, bitsPerSample, frameNumber, options, &writer)
}

func encodeFrameWithWriter(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options flac.EncoderConfig, w *bits.Writer) ([]byte, error) {
	if bitsPerSample < 4 || bitsPerSample > 32 {
		return nil, fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	if sampleRate < 1 || sampleRate > 1048575 {
		return nil, fmt.Errorf("invalid FLAC sample rate: %d", sampleRate)
	}
	if options.MaxRicePartitionOrder < 0 || options.MaxRicePartitionOrder > 15 || options.StreamableSubset && options.MaxRicePartitionOrder > 8 {
		return nil, fmt.Errorf("invalid FLAC Rice partition order: %d", options.MaxRicePartitionOrder)
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
	if options.StreamableSubset {
		if maxBlockSize := streamableMaxBlockSize(sampleRate); blockSize > maxBlockSize {
			return nil, fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", blockSize, maxBlockSize, sampleRate)
		}
		if sampleRate <= 48000 && options.MaxLPCOrder > streamableMaxLPCOrder {
			options.MaxLPCOrder = streamableMaxLPCOrder
		}
	}
	for ch := range samples {
		if len(samples[ch]) != blockSize {
			return nil, fmt.Errorf("FLAC channel %d has mismatched block size", ch)
		}
		if err := flac.ValidateSampleRange(samples[ch], bitsPerSample); err != nil {
			return nil, err
		}
	}

	w.Init()
	assignment, channels, candidates, err := chooseChannelAssignment(samples, bitsPerSample, options)
	if err != nil {
		return nil, err
	}
	defer releaseSubframeCandidates(candidates)

	header := &flac.FrameHeader{
		BlockSize:         blockSize,
		SampleRate:        sampleRate,
		ChannelAssignment: assignmentForChannels(assignment, len(samples)),
		BitsPerSample:     bitsPerSample,
		Number:            frameNumber,
		BlockingStrategy:  options.BlockingStrategy == flac.VariableBlocking,
	}

	if err := EncodeFrameHeader(w, header, options.StreamableSubset); err != nil {
		return nil, err
	}
	for ch := range channels {
		channelBits := bitsPerSample
		if assignment == 8 && ch == 1 || assignment == 9 && ch == 0 || assignment == 10 && ch == 1 {
			channelBits++
		}
		if err := EncodeSubframeCandidate(w, channels[ch], channelBits, candidates[ch]); err != nil {
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

const streamableMaxLPCOrder = 12

func streamableMaxBlockSize(sampleRate int) int {
	if sampleRate <= 48000 {
		return 4608
	}
	return 16384
}

func chooseChannelAssignment(samples [][]int64, bitsPerSample int, options flac.EncoderConfig) (uint8, [][]int64, []subframeCandidate, error) {
	if !options.EnableStereoDecorrelation || len(samples) != 2 {
		candidates := make([]subframeCandidate, len(samples))
		for ch := range samples {
			candidates[ch] = bestSubframe(samples[ch], bitsPerSample, options)
			if !candidates[ch].valid {
				releaseSubframeCandidates(candidates)
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
		releaseSubframeCandidate(&leftCandidate)
		releaseSubframeCandidate(&rightCandidate)
		releaseSubframeCandidate(&midCandidate)
		releaseSubframeCandidate(&sideCandidate)
		return 0, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
	}
	chosen := assignments[bestIndex]
	switch bestIndex {
	case 0:
		releaseSubframeCandidate(&midCandidate)
		releaseSubframeCandidate(&sideCandidate)
	case 1:
		releaseSubframeCandidate(&rightCandidate)
		releaseSubframeCandidate(&midCandidate)
	case 2:
		releaseSubframeCandidate(&leftCandidate)
		releaseSubframeCandidate(&midCandidate)
	case 3:
		releaseSubframeCandidate(&leftCandidate)
		releaseSubframeCandidate(&rightCandidate)
	}
	return chosen.assignment, chosen.channels, chosen.candidates, nil
}
