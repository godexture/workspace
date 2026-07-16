package encoder

import (
	"fmt"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func EncodeFrame(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options flac.EncoderConfig) ([]byte, error) {
	var writer bits.Writer
	windows := newWindowSet(options.Apodizations)
	return encodeFrameWithWriter(samples, sampleRate, bitsPerSample, frameNumber, options, false, &windows, &writer)
}

func encodeFrameWithWriter(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options flac.EncoderConfig, samplesValidated bool, windows *windowSet, w *bits.Writer) ([]byte, error) {
	if bitsPerSample < 4 || bitsPerSample > 32 {
		return nil, fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	if sampleRate < 1 || sampleRate > 1048575 {
		return nil, fmt.Errorf("invalid FLAC sample rate: %d", sampleRate)
	}
	if options.MaxRicePartitionOrder < 0 || options.MaxRicePartitionOrder > 15 || options.StreamableSubset && options.MaxRicePartitionOrder > 8 {
		return nil, fmt.Errorf("invalid FLAC Rice partition order: %d", options.MaxRicePartitionOrder)
	}
	if options.LPCPrecision != 0 && (options.LPCPrecision < 4 || options.LPCPrecision > 15) {
		return nil, fmt.Errorf("invalid FLAC LPC precision: %d", options.LPCPrecision)
	}
	if options.StereoMode > flac.StereoExhaustive {
		return nil, fmt.Errorf("invalid FLAC stereo mode: %d", options.StereoMode)
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
		if !samplesValidated {
			if err := flac.ValidateSampleRange(samples[ch], bitsPerSample); err != nil {
				return nil, err
			}
		}
	}

	w.Init()
	assignment, channels, candidates, scratch, err := chooseChannelAssignment(samples, bitsPerSample, options, windows)
	if err != nil {
		return nil, err
	}
	defer releaseSubframeCandidates(candidates)
	defer releaseResidualBuffers(scratch)

	header := &frame.Header{
		BlockSize:         blockSize,
		SampleRate:        sampleRate,
		ChannelAssignment: assignmentForChannels(assignment, len(samples)),
		BitsPerSample:     bitsPerSample,
		Number:            frameNumber,
		BlockingStrategy:  options.BlockingStrategy == flac.VariableBlocking,
	}

	if err := frame.EncodeHeader(w, header, options.StreamableSubset); err != nil {
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
	crc := hash.CRC16(w.Bytes())
	w.Byte(byte(crc >> 8))
	w.Byte(byte(crc))
	return w.Bytes(), nil
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

func chooseChannelAssignment(samples [][]int64, bitsPerSample int, options flac.EncoderConfig, windows *windowSet) (uint8, [][]int64, []subframeCandidate, [][]int64, error) {
	frameWindows := windows.forLength(len(samples[0]))
	if options.StereoMode == flac.StereoIndependent || len(samples) != 2 {
		candidates := make([]subframeCandidate, len(samples))
		for ch := range samples {
			candidates[ch] = bestSubframe(samples[ch], bitsPerSample, options, frameWindows)
			if !candidates[ch].valid {
				releaseSubframeCandidates(candidates)
				return 0, nil, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
			}
		}
		return 0, samples, candidates, nil, nil
	}

	left, right := samples[0], samples[1]
	mid := getResidualBuffer(len(left))
	side := getResidualBuffer(len(left))
	scratch := [][]int64{mid, side}
	for i := range left {
		mid[i] = (left[i] + right[i]) >> 1
		side[i] = left[i] - right[i]
	}
	if options.StereoMode == flac.StereoAdaptive && len(left) >= 3 {
		assignment := estimateStereoAssignment(left, right, mid, side)
		channels := assignmentChannels(assignment, left, right, mid, side)
		candidates := []subframeCandidate{
			bestSubframe(channels[0], bitsPerSample+sideChannelOffset(assignment, 0), options, frameWindows),
			bestSubframe(channels[1], bitsPerSample+sideChannelOffset(assignment, 1), options, frameWindows),
		}
		if candidates[0].valid && candidates[1].valid {
			return assignment, channels, candidates, scratch, nil
		}
		releaseSubframeCandidates(candidates)
		releaseResidualBuffers(scratch)
		return 0, nil, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
	}

	leftCandidate := bestSubframe(left, bitsPerSample, options, frameWindows)
	rightCandidate := bestSubframe(right, bitsPerSample, options, frameWindows)
	midCandidate := bestSubframe(mid, bitsPerSample, options, frameWindows)
	sideCandidate := bestSubframe(side, bitsPerSample+1, options, frameWindows)

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
		releaseResidualBuffers(scratch)
		return 0, nil, nil, nil, fmt.Errorf("no valid FLAC channel assignment")
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
	return chosen.assignment, chosen.channels, chosen.candidates, scratch, nil
}

func assignmentChannels(assignment uint8, left, right, mid, side []int64) [][]int64 {
	switch assignment {
	case 8:
		return [][]int64{left, side}
	case 9:
		return [][]int64{side, right}
	case 10:
		return [][]int64{mid, side}
	default:
		return [][]int64{left, right}
	}
}

func sideChannelOffset(assignment uint8, channel int) int {
	if assignment == 8 && channel == 1 || assignment == 9 && channel == 0 || assignment == 10 && channel == 1 {
		return 1
	}
	return 0
}

func estimateStereoAssignment(left, right, mid, side []int64) uint8 {
	leftBits, rightBits := estimateChannelBits(left), estimateChannelBits(right)
	midBits, sideBits := estimateChannelBits(mid), estimateChannelBits(side)
	best, assignment := leftBits+rightBits, uint8(0)
	for _, candidate := range []struct {
		assignment uint8
		cost       uint64
	}{{8, leftBits + sideBits}, {9, sideBits + rightBits}, {10, midBits + sideBits}} {
		if candidate.cost < best {
			best, assignment = candidate.cost, candidate.assignment
		}
	}
	return assignment
}

func estimateChannelBits(samples []int64) uint64 {
	residual := fixedResidual(samples, 2)
	defer releaseResidualBuffer(residual)
	var sum, maximum uint64
	for _, value := range residual {
		folded := foldResidual(value)
		sum += folded
		if folded > maximum {
			maximum = folded
		}
	}
	return bestRicePartitionEstimate(sum, maximum, len(residual), 4, 14).costBits
}

func releaseResidualBuffers(buffers [][]int64) {
	for _, buffer := range buffers {
		releaseResidualBuffer(buffer)
	}
}
