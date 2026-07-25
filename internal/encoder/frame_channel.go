package encoder

import (
	"errors"

	"github.com/godexture/codec-flac/internal/config"
)

var errNoChannelAssignment = errors.New("no valid FLAC channel assignment")

func assignmentForChannels(assignment uint8, channels int) uint8 {
	if assignment != 0 {
		return assignment
	}
	return uint8(channels - 1)
}

const streamableMaxLPCOrder = 12

func chooseChannelAssignment(samples [][]int64, bitsPerSample int, options config.EncoderConfig, windows *windowSet) (uint8, [][]int64, []subframeCandidate, [][]int64, error) {
	frameWindows := windows.forLength(len(samples[0]))
	if options.StereoMode == config.StereoIndependent || len(samples) != 2 {
		candidates := make([]subframeCandidate, len(samples))
		for ch := range samples {
			candidates[ch] = bestSubframe(samples[ch], bitsPerSample, options, frameWindows, &windows.lpc)
			if !candidates[ch].valid {
				releaseSubframeCandidates(candidates)
				return 0, nil, nil, nil, errNoChannelAssignment
			}
		}
		return 0, samples, candidates, nil, nil
	}

	left, right := samples[0], samples[1]
	mid := getResidualBuffer(len(left))
	side := getResidualBuffer(len(left))
	scratch := [][]int64{mid, side}
	computeMidSide(left, right, mid, side)
	if options.StereoMode == config.StereoAdaptive && len(left) >= 3 {
		assignment := estimateStereoAssignment(left, right, mid, side)
		channels := assignmentChannels(assignment, left, right, mid, side)
		candidates := []subframeCandidate{
			bestSubframe(channels[0], bitsPerSample+sideChannelOffset(assignment, 0), options, frameWindows, &windows.lpc),
			bestSubframe(channels[1], bitsPerSample+sideChannelOffset(assignment, 1), options, frameWindows, &windows.lpc),
		}
		if candidates[0].valid && candidates[1].valid {
			return assignment, channels, candidates, scratch, nil
		}
		releaseSubframeCandidates(candidates)
		releaseResidualBuffers(scratch)
		return 0, nil, nil, nil, errNoChannelAssignment
	}

	leftCandidate := bestSubframe(left, bitsPerSample, options, frameWindows, &windows.lpc)
	rightCandidate := bestSubframe(right, bitsPerSample, options, frameWindows, &windows.lpc)
	midCandidate := bestSubframe(mid, bitsPerSample, options, frameWindows, &windows.lpc)
	sideCandidate := bestSubframe(side, bitsPerSample+1, options, frameWindows, &windows.lpc)

	assignments := make([]struct {
		assignment uint8
		candidates []subframeCandidate
	}, 0, 4)
	for _, assignment := range []uint8{0, 8, 9, 10} {
		var candidates []subframeCandidate
		switch assignment {
		case 0:
			candidates = []subframeCandidate{leftCandidate, rightCandidate}
		case 8:
			candidates = []subframeCandidate{leftCandidate, sideCandidate}
		case 9:
			candidates = []subframeCandidate{sideCandidate, rightCandidate}
		case 10:
			candidates = []subframeCandidate{midCandidate, sideCandidate}
		}
		assignments = append(assignments, struct {
			assignment uint8
			candidates []subframeCandidate
		}{assignment, candidates})
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
		return 0, nil, nil, nil, errNoChannelAssignment
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
	return chosen.assignment, assignmentChannels(chosen.assignment, left, right, mid, side), chosen.candidates, scratch, nil
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
	sum, maximum := foldSumMax(residual)
	return bestRicePartitionEstimate(sum, maximum, len(residual), 4, 14).costBits
}

func releaseResidualBuffers(buffers [][]int64) {
	for _, buffer := range buffers {
		releaseResidualBuffer(buffer)
	}
}
