package mp4

import (
	"context"
	"fmt"

	"github.com/godexture/godec/access"
)

// parseSampleTable records the fixed set of required table ranges, then scans
// them without retaining their entries.
func parseSampleTable(ctx context.Context, reader access.Random, sourceEnd uint64, value box, result *track, dataReferences uint32) error {
	var haveDescriptions, haveTiming, haveLayout, haveSizes, haveOffsets bool
	err := scanChildBoxes(ctx, reader, sourceEnd, value, func(child box) error {
		switch child.typeID {
		case typeSTSD:
			if haveDescriptions {
				return fmt.Errorf("%w: stsd is repeated", errMalformedMovie)
			}
			result.tables.description = child
			haveDescriptions = true
		case typeSTTS:
			if haveTiming {
				return fmt.Errorf("%w: stts is repeated", errMalformedMovie)
			}
			result.tables.timing = child
			haveTiming = true
		case typeCTTS:
			if result.tables.hasComposition {
				return fmt.Errorf("%w: ctts is repeated", errMalformedMovie)
			}
			result.tables.composition = child
			result.tables.hasComposition = true
		case typeSTSC:
			if haveLayout {
				return fmt.Errorf("%w: stsc is repeated", errMalformedMovie)
			}
			result.tables.layout = child
			haveLayout = true
		case typeSTSZ, typeSTZ2:
			if haveSizes {
				return fmt.Errorf("%w: stsz or stz2 is repeated", errMalformedMovie)
			}
			result.tables.sizes = child
			result.tables.compactSizes = child.typeID == typeSTZ2
			haveSizes = true
		case typeSTCO, typeCO64:
			if haveOffsets {
				return fmt.Errorf("%w: stco or co64 is repeated", errMalformedMovie)
			}
			result.tables.offsets = child
			result.tables.largeOffsets = child.typeID == typeCO64
			haveOffsets = true
		case typeSTSS:
			if result.tables.hasSync {
				return fmt.Errorf("%w: stss is repeated", errMalformedMovie)
			}
			result.tables.sync = child
			result.tables.hasSync = true
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
	descriptionCount, codec, err := scanSampleDescriptions(ctx, reader, result.tables.description, dataReferences)
	if err != nil {
		return err
	}
	sampleCount, duration, err := scanTimingTable(ctx, reader, result.tables.timing)
	if err != nil {
		return err
	}
	sizeCount, maxSize, fixedSize, err := scanSampleSizes(ctx, reader, result.tables.sizes, result.tables.compactSizes)
	if err != nil {
		return err
	}
	if sampleCount != sizeCount {
		return fmt.Errorf("%w: stts and sample-size counts disagree", errMalformedMovie)
	}
	offsetCount, err := scanChunkOffsets(ctx, reader, result.tables.offsets, result.tables.largeOffsets)
	if err != nil {
		return err
	}
	if err := scanChunkLayout(ctx, reader, result.tables.layout, descriptionCount, offsetCount, sampleCount); err != nil {
		return err
	}
	if result.tables.hasComposition {
		compositionCount, err := scanCompositionTable(ctx, reader, result.tables.composition)
		if err != nil {
			return err
		}
		if compositionCount != sampleCount {
			return fmt.Errorf("%w: ctts and sample counts disagree", errMalformedMovie)
		}
	}
	if result.tables.hasSync {
		if err := scanSyncSamples(ctx, reader, result.tables.sync, sampleCount); err != nil {
			return err
		}
	}
	result.descriptionCount = descriptionCount
	result.codec = codec
	result.sampleCount = sampleCount
	result.duration = duration
	result.maxSampleSize = maxSize
	result.tables.fixedSize = fixedSize
	return nil
}
