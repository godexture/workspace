package mp4

import (
	"fmt"
	"math"
)

func expandTrack(value *track, media []mediaRange, budget *movieBudget) error {
	sampleCount, err := timingSampleCount(value.timing)
	if err != nil {
		return err
	}
	if sampleCount != uint64(len(value.sampleSizes)) {
		return fmt.Errorf("%w: stts and sample-size counts disagree", errMalformedMovie)
	}
	if err := validateCompositionCount(value.composition, sampleCount); err != nil {
		return err
	}
	if err := budget.reserveRecords(sampleCount, sampleBudgetBytes, "sample model"); err != nil {
		return err
	}
	samples, err := expandChunks(value, media, sampleCount)
	if err != nil {
		return err
	}
	if err := assignTiming(samples, value.timing); err != nil {
		return err
	}
	if err := assignComposition(samples, value.composition); err != nil {
		return err
	}
	if err := assignSync(samples, value.syncSamples, value.hasSyncSample); err != nil {
		return err
	}
	value.samples = samples
	budget.releaseRecords(uint64(len(value.timing)), 16)
	budget.releaseRecords(uint64(len(value.composition)), 16)
	budget.releaseRecords(uint64(len(value.chunkLayout)), 16)
	budget.releaseRecords(uint64(len(value.chunkOffsets)), 8)
	budget.releaseRecords(uint64(len(value.sampleSizes)), 4)
	budget.releaseRecords(uint64(len(value.syncSamples)), 4)
	value.timing = nil
	value.composition = nil
	value.chunkLayout = nil
	value.chunkOffsets = nil
	value.sampleSizes = nil
	value.syncSamples = nil
	value.hasSyncSample = false
	return nil
}

func timingSampleCount(values []timingRun) (uint64, error) {
	var total uint64
	for _, value := range values {
		next, ok := checkedBoxAdd(total, uint64(value.count))
		if !ok {
			return 0, fmt.Errorf("%w: stts sample count overflows", errMalformedMovie)
		}
		total = next
	}
	return total, nil
}

func validateCompositionCount(values []compositionRun, sampleCount uint64) error {
	if values == nil {
		return nil
	}
	var total uint64
	for _, value := range values {
		next, ok := checkedBoxAdd(total, uint64(value.count))
		if !ok {
			return fmt.Errorf("%w: ctts sample count overflows", errMalformedMovie)
		}
		total = next
	}
	if total != sampleCount {
		return fmt.Errorf("%w: ctts and sample counts disagree", errMalformedMovie)
	}
	return nil
}

func expandChunks(value *track, media []mediaRange, sampleCount uint64) ([]sample, error) {
	if sampleCount == 0 {
		if len(value.chunkLayout) != 0 || len(value.chunkOffsets) != 0 {
			return nil, fmt.Errorf("%w: empty track has chunks", errMalformedMovie)
		}
		return []sample{}, nil
	}
	if len(value.chunkLayout) == 0 || len(value.chunkOffsets) == 0 {
		return nil, fmt.Errorf("%w: non-empty track has no chunk layout", errMalformedMovie)
	}
	if value.chunkLayout[0].firstChunk != 1 {
		return nil, fmt.Errorf("%w: first stsc chunk is not one", errMalformedMovie)
	}
	for index, run := range value.chunkLayout {
		if run.samplesPerChunk == 0 || run.descriptionIndex == 0 || uint64(run.descriptionIndex) > uint64(len(value.descriptions)) {
			return nil, fmt.Errorf("%w: stsc entry %d", errMalformedMovie, index+1)
		}
		if uint64(run.firstChunk) > uint64(len(value.chunkOffsets)) {
			return nil, fmt.Errorf("%w: stsc entry %d starts after the last chunk", errMalformedMovie, index+1)
		}
		if index != 0 && run.firstChunk <= value.chunkLayout[index-1].firstChunk {
			return nil, fmt.Errorf("%w: stsc first_chunk is not ascending", errMalformedMovie)
		}
	}
	if sampleCount > uint64(math.MaxInt) {
		return nil, fmt.Errorf("%w: sample count exceeds runtime range", errUnsupportedMovie)
	}
	result := make([]sample, 0, int(sampleCount))
	for runIndex, run := range value.chunkLayout {
		first := uint64(run.firstChunk - 1)
		last := uint64(len(value.chunkOffsets))
		if runIndex+1 < len(value.chunkLayout) {
			last = uint64(value.chunkLayout[runIndex+1].firstChunk - 1)
		}
		for chunkIndex := first; chunkIndex < last; chunkIndex++ {
			offset := value.chunkOffsets[chunkIndex]
			for sampleIndex := uint32(0); sampleIndex < run.samplesPerChunk; sampleIndex++ {
				if uint64(len(result)) == sampleCount {
					return nil, fmt.Errorf("%w: stsc describes too many samples", errMalformedMovie)
				}
				size := value.sampleSizes[len(result)]
				if !withinMedia(media, offset, uint64(size)) {
					return nil, fmt.Errorf("%w: sample %d lies outside mdat payload", errMalformedMovie, len(result)+1)
				}
				result = append(result, sample{offset: offset, size: size, descriptionIndex: run.descriptionIndex})
				next, ok := checkedBoxAdd(offset, uint64(size))
				if !ok {
					return nil, fmt.Errorf("%w: sample offset overflows", errMalformedMovie)
				}
				offset = next
			}
		}
	}
	if uint64(len(result)) != sampleCount {
		return nil, fmt.Errorf("%w: stsc and sample counts disagree", errMalformedMovie)
	}
	return result, nil
}

func withinMedia(values []mediaRange, offset, size uint64) bool {
	end, ok := checkedBoxAdd(offset, size)
	if !ok {
		return false
	}
	for _, value := range values {
		if offset >= value.start && end <= value.end {
			return true
		}
	}
	return false
}

func assignTiming(samples []sample, runs []timingRun) error {
	index := 0
	var dts uint64
	for _, run := range runs {
		for count := uint32(0); count < run.count; count++ {
			if index == len(samples) {
				return fmt.Errorf("%w: stts describes too many samples", errMalformedMovie)
			}
			samples[index].dts = dts
			samples[index].duration = run.duration
			next, ok := checkedBoxAdd(dts, uint64(run.duration))
			if !ok {
				return fmt.Errorf("%w: DTS overflows", errMalformedMovie)
			}
			dts = next
			index++
		}
	}
	if index != len(samples) {
		return fmt.Errorf("%w: stts and sample counts disagree", errMalformedMovie)
	}
	return nil
}

func assignComposition(samples []sample, runs []compositionRun) error {
	if runs == nil {
		for index := range samples {
			if samples[index].dts > math.MaxInt64 {
				return fmt.Errorf("%w: PTS exceeds runtime range", errUnsupportedMovie)
			}
			samples[index].pts = int64(samples[index].dts)
		}
		return nil
	}
	index := 0
	for _, run := range runs {
		for count := uint32(0); count < run.count; count++ {
			if index == len(samples) {
				return fmt.Errorf("%w: ctts describes too many samples", errMalformedMovie)
			}
			pts, ok := addCompositionOffset(samples[index].dts, run.offset)
			if !ok {
				return fmt.Errorf("%w: PTS overflows", errMalformedMovie)
			}
			samples[index].pts = pts
			index++
		}
	}
	if index != len(samples) {
		return fmt.Errorf("%w: ctts and sample counts disagree", errMalformedMovie)
	}
	return nil
}

func addCompositionOffset(dts uint64, offset int64) (int64, bool) {
	if dts > math.MaxInt64 {
		return 0, false
	}
	base := int64(dts)
	if offset > 0 && base > math.MaxInt64-offset || offset < 0 && base < math.MinInt64-offset {
		return 0, false
	}
	return base + offset, true
}

func assignSync(samples []sample, syncSamples []uint32, hasTable bool) error {
	if !hasTable {
		for index := range samples {
			samples[index].sync = true
		}
		return nil
	}
	previous := uint32(0)
	for _, number := range syncSamples {
		if number == 0 || number <= previous || uint64(number) > uint64(len(samples)) {
			return fmt.Errorf("%w: stss sample number", errMalformedMovie)
		}
		samples[number-1].sync = true
		previous = number
	}
	return nil
}
