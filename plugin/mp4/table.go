package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

func parseSampleTable(ctx context.Context, reader access.Random, sourceEnd uint64, value box, budget *movieBudget, result *track, dataReferences uint32) error {
	var haveDescriptions, haveTiming, haveLayout, haveSizes, haveOffsets bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeSTSD:
			if haveDescriptions {
				return fmt.Errorf("%w: stsd is repeated", errMalformedMovie)
			}
			descriptions, err := parseSampleDescriptions(ctx, reader, child, budget, dataReferences)
			if err != nil {
				return err
			}
			result.descriptions = descriptions
			haveDescriptions = true
		case typeSTTS:
			if haveTiming {
				return fmt.Errorf("%w: stts is repeated", errMalformedMovie)
			}
			timing, err := parseTimingTable(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.timing = timing
			haveTiming = true
		case typeCTTS:
			if result.composition != nil {
				return fmt.Errorf("%w: ctts is repeated", errMalformedMovie)
			}
			composition, err := parseCompositionTable(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.composition = composition
		case typeSTSC:
			if haveLayout {
				return fmt.Errorf("%w: stsc is repeated", errMalformedMovie)
			}
			layout, err := parseChunkLayout(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.chunkLayout = layout
			haveLayout = true
		case typeSTSZ:
			if haveSizes {
				return fmt.Errorf("%w: stsz or stz2 is repeated", errMalformedMovie)
			}
			sizes, err := parseSampleSizes(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.sampleSizes = sizes
			haveSizes = true
		case typeSTZ2:
			if haveSizes {
				return fmt.Errorf("%w: stsz or stz2 is repeated", errMalformedMovie)
			}
			sizes, err := parseCompactSampleSizes(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.sampleSizes = sizes
			haveSizes = true
		case typeSTCO:
			if haveOffsets {
				return fmt.Errorf("%w: stco or co64 is repeated", errMalformedMovie)
			}
			offsets, err := parseChunkOffsets(ctx, reader, child, budget, false)
			if err != nil {
				return err
			}
			result.chunkOffsets = offsets
			haveOffsets = true
		case typeCO64:
			if haveOffsets {
				return fmt.Errorf("%w: stco or co64 is repeated", errMalformedMovie)
			}
			offsets, err := parseChunkOffsets(ctx, reader, child, budget, true)
			if err != nil {
				return err
			}
			result.chunkOffsets = offsets
			haveOffsets = true
		case typeSTSS:
			if result.hasSyncSample {
				return fmt.Errorf("%w: stss is repeated", errMalformedMovie)
			}
			syncSamples, err := parseSyncSamples(ctx, reader, child, budget)
			if err != nil {
				return err
			}
			result.syncSamples = syncSamples
			result.hasSyncSample = true
		case typeMOOF, typeMVEX:
			return fmt.Errorf("%w: fragmented sample table", errUnsupportedMovie)
		case typeEDTS, typeELST:
			return fmt.Errorf("%w: edit list in sample table", errUnsupportedMovie)
		default:
			return fmt.Errorf("%w: stbl child %q", errUnsupportedMovie, child.typeID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !haveDescriptions || !haveTiming || !haveLayout || !haveSizes || !haveOffsets {
		return fmt.Errorf("%w: stbl requires stsd, stts, stsc, stsz/stz2, and stco/co64", errMalformedMovie)
	}
	return nil
}

func parseSampleDescriptions(ctx context.Context, reader access.Random, value box, budget *movieBudget, dataReferences uint32) ([]sampleDescription, error) {
	if err := budget.reserve(value.payloadSize, "stsd backing"); err != nil {
		return nil, err
	}
	data, err := readBoxData(ctx, reader, value, budget, "stsd")
	if err != nil {
		return nil, err
	}
	version, flags, err := fullBox(data, "stsd")
	if err != nil {
		return nil, err
	}
	if version != 0 || flags != 0 || len(data) < 8 {
		return nil, fmt.Errorf("%w: stsd full-box header", errUnsupportedMovie)
	}
	count := binary.BigEndian.Uint32(data[4:8])
	if count == 0 {
		return nil, fmt.Errorf("%w: stsd has no entries", errMalformedMovie)
	}
	if err := budget.reserveRecords(uint64(count), descriptionBudgetBytes, "sample descriptions"); err != nil {
		return nil, err
	}
	result := make([]sampleDescription, 0, int(count))
	for offset, index := 8, uint32(0); index < count; index++ {
		if len(data)-offset < 16 {
			return nil, fmt.Errorf("%w: stsd entry %d is truncated", errMalformedMovie, index+1)
		}
		size := binary.BigEndian.Uint32(data[offset : offset+4])
		if size < 16 || uint64(size) > uint64(len(data)-offset) {
			return nil, fmt.Errorf("%w: stsd entry %d size", errMalformedMovie, index+1)
		}
		end := offset + int(size)
		typeID := boxType(data[offset+4 : offset+8])
		if typeID == typeENCV || typeID == typeENCA {
			return nil, fmt.Errorf("%w: encrypted sample entry %q", errUnsupportedMovie, typeID)
		}
		dataReference := binary.BigEndian.Uint16(data[offset+14 : offset+16])
		if dataReference == 0 || uint32(dataReference) > dataReferences {
			return nil, fmt.Errorf("%w: stsd entry %d data reference", errMalformedMovie, index+1)
		}
		result = append(result, sampleDescription{
			typeID:             typeID,
			dataReferenceIndex: dataReference,
			raw:                data[offset:end],
		})
		offset = end
	}
	if offset := 8 + sampleDescriptionBytes(result); offset != len(data) {
		return nil, fmt.Errorf("%w: stsd has trailing bytes", errMalformedMovie)
	}
	return result, nil
}

func sampleDescriptionBytes(values []sampleDescription) int {
	total := 0
	for _, value := range values {
		total += len(value.raw)
	}
	return total
}

func parseTimingTable(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]timingRun, error) {
	data, count, err := readTable(ctx, reader, value, budget, "stts", 8, 0, false)
	if err != nil {
		return nil, err
	}
	if err := budget.reserveRecords(uint64(count), 16, "stts entries"); err != nil {
		return nil, err
	}
	result := make([]timingRun, count)
	for index := range result {
		offset := 8 + index*8
		result[index] = timingRun{count: binary.BigEndian.Uint32(data[offset:]), duration: binary.BigEndian.Uint32(data[offset+4:])}
		if result[index].count == 0 {
			return nil, fmt.Errorf("%w: stts entry %d has zero samples", errMalformedMovie, index+1)
		}
	}
	return result, nil
}

func parseCompositionTable(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]compositionRun, error) {
	data, count, err := readTable(ctx, reader, value, budget, "ctts", 8, 0, true)
	if err != nil {
		return nil, err
	}
	version := data[0]
	if version > 1 {
		return nil, fmt.Errorf("%w: ctts version %d", errUnsupportedMovie, version)
	}
	if err := budget.reserveRecords(uint64(count), 16, "ctts entries"); err != nil {
		return nil, err
	}
	result := make([]compositionRun, count)
	for index := range result {
		offset := 8 + index*8
		compositionOffset := binary.BigEndian.Uint32(data[offset+4:])
		if version == 0 {
			result[index] = compositionRun{count: binary.BigEndian.Uint32(data[offset:]), offset: int64(compositionOffset)}
		} else {
			result[index] = compositionRun{count: binary.BigEndian.Uint32(data[offset:]), offset: int64(int32(compositionOffset))}
		}
		if result[index].count == 0 {
			return nil, fmt.Errorf("%w: ctts entry %d has zero samples", errMalformedMovie, index+1)
		}
	}
	return result, nil
}

func parseChunkLayout(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]chunkRun, error) {
	data, count, err := readTable(ctx, reader, value, budget, "stsc", 12, 0, false)
	if err != nil {
		return nil, err
	}
	if err := budget.reserveRecords(uint64(count), 16, "stsc entries"); err != nil {
		return nil, err
	}
	result := make([]chunkRun, count)
	for index := range result {
		offset := 8 + index*12
		result[index] = chunkRun{
			firstChunk:       binary.BigEndian.Uint32(data[offset:]),
			samplesPerChunk:  binary.BigEndian.Uint32(data[offset+4:]),
			descriptionIndex: binary.BigEndian.Uint32(data[offset+8:]),
		}
	}
	return result, nil
}

func parseSampleSizes(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]uint32, error) {
	data, err := readBoxData(ctx, reader, value, budget, "stsz")
	if err != nil {
		return nil, err
	}
	version, flags, err := fullBox(data, "stsz")
	if err != nil {
		return nil, err
	}
	if version != 0 || flags != 0 || len(data) < 12 {
		return nil, fmt.Errorf("%w: stsz full-box header", errUnsupportedMovie)
	}
	size := binary.BigEndian.Uint32(data[4:8])
	count := binary.BigEndian.Uint32(data[8:12])
	entries := uint64(0)
	if size == 0 {
		entries = uint64(count) * 4
	}
	if entries > uint64(math.MaxInt)-12 || len(data) != 12+int(entries) {
		return nil, fmt.Errorf("%w: stsz entries", errMalformedMovie)
	}
	if err := budget.reserveRecords(uint64(count), 4, "sample sizes"); err != nil {
		return nil, err
	}
	result := make([]uint32, count)
	for index := range result {
		if size == 0 {
			result[index] = binary.BigEndian.Uint32(data[12+index*4:])
		} else {
			result[index] = size
		}
	}
	return result, nil
}

func parseCompactSampleSizes(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]uint32, error) {
	data, err := readBoxData(ctx, reader, value, budget, "stz2")
	if err != nil {
		return nil, err
	}
	version, flags, err := fullBox(data, "stz2")
	if err != nil {
		return nil, err
	}
	if version != 0 || flags != 0 || len(data) < 12 {
		return nil, fmt.Errorf("%w: stz2 full-box header", errUnsupportedMovie)
	}
	bits := data[7]
	if bits != 4 && bits != 8 && bits != 16 {
		return nil, fmt.Errorf("%w: stz2 field size %d", errUnsupportedMovie, bits)
	}
	count := binary.BigEndian.Uint32(data[8:12])
	bytes, ok := compactSizeBytes(uint64(count), bits)
	if !ok || bytes > uint64(math.MaxInt)-12 || len(data) != 12+int(bytes) {
		return nil, fmt.Errorf("%w: stz2 entries", errMalformedMovie)
	}
	if err := budget.reserveRecords(uint64(count), 4, "compact sample sizes"); err != nil {
		return nil, err
	}
	result := make([]uint32, count)
	for index := range result {
		switch bits {
		case 4:
			value := data[12+index/2]
			if index&1 == 0 {
				result[index] = uint32(value >> 4)
			} else {
				result[index] = uint32(value & 0x0f)
			}
		case 8:
			result[index] = uint32(data[12+index])
		case 16:
			result[index] = uint32(binary.BigEndian.Uint16(data[12+index*2:]))
		}
	}
	return result, nil
}

func compactSizeBytes(count uint64, bits byte) (uint64, bool) {
	bitsTotal := count * uint64(bits)
	if count != 0 && bitsTotal/uint64(bits) != count {
		return 0, false
	}
	return (bitsTotal + 7) / 8, bitsTotal <= math.MaxUint64-7
}

func parseChunkOffsets(ctx context.Context, reader access.Random, value box, budget *movieBudget, large bool) ([]uint64, error) {
	entrySize := uint64(4)
	what := "stco"
	if large {
		entrySize = 8
		what = "co64"
	}
	data, count, err := readTable(ctx, reader, value, budget, what, entrySize, 0, false)
	if err != nil {
		return nil, err
	}
	if err := budget.reserveRecords(uint64(count), 8, "chunk offsets"); err != nil {
		return nil, err
	}
	result := make([]uint64, count)
	for index := range result {
		offset := 8 + uint64(index)*entrySize
		if large {
			result[index] = binary.BigEndian.Uint64(data[offset:])
		} else {
			result[index] = uint64(binary.BigEndian.Uint32(data[offset:]))
		}
	}
	return result, nil
}

func parseSyncSamples(ctx context.Context, reader access.Random, value box, budget *movieBudget) ([]uint32, error) {
	data, count, err := readTable(ctx, reader, value, budget, "stss", 4, 0, false)
	if err != nil {
		return nil, err
	}
	if err := budget.reserveRecords(uint64(count), 4, "sync samples"); err != nil {
		return nil, err
	}
	result := make([]uint32, count)
	for index := range result {
		result[index] = binary.BigEndian.Uint32(data[8+index*4:])
	}
	return result, nil
}

func readTable(ctx context.Context, reader access.Random, value box, budget *movieBudget, what string, entrySize uint64, expectedVersion byte, allowVersionOne bool) ([]byte, uint32, error) {
	data, err := readBoxData(ctx, reader, value, budget, what)
	if err != nil {
		return nil, 0, err
	}
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("%w: %s has no entry count", errMalformedMovie, what)
	}
	version, flags, err := fullBox(data, what)
	if err != nil {
		return nil, 0, err
	}
	if flags != 0 || version != expectedVersion && !(allowVersionOne && version == 1) || len(data) < 8 {
		return nil, 0, fmt.Errorf("%w: %s full-box header", errUnsupportedMovie, what)
	}
	count := binary.BigEndian.Uint32(data[4:8])
	if uint64(count) > (uint64(math.MaxInt)-8)/entrySize || len(data) != 8+int(uint64(count)*entrySize) {
		return nil, 0, fmt.Errorf("%w: %s entries", errMalformedMovie, what)
	}
	return data, count, nil
}
