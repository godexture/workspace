package mp4

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/access"
)

func scanChunkOffsets(ctx context.Context, reader access.Random, value box, large bool) (uint32, error) {
	entrySize := uint64(4)
	what := "stco"
	if large {
		entrySize = 8
		what = "co64"
	}
	entries, err := newTableReader(ctx, reader, value, what, entrySize, 0, false)
	if err != nil {
		return 0, err
	}
	count := entries.remaining
	for entries.remaining > 0 {
		if _, err := entries.next(ctx); err != nil {
			return 0, err
		}
	}
	return count, nil
}

func scanChunkLayout(ctx context.Context, reader access.Random, value box, descriptions, chunks uint32, samples uint64) error {
	entries, err := newTableReader(ctx, reader, value, "stsc", 12, 0, false)
	if err != nil {
		return err
	}
	if samples != 0 && entries.remaining == 0 {
		return fmt.Errorf("%w: non-empty track has no chunk layout", errMalformedMovie)
	}
	entryCount := entries.remaining
	var previous chunkRun
	var described uint64
	for index := uint32(0); index < entryCount; index++ {
		entry, err := entries.next(ctx)
		if err != nil {
			return err
		}
		current := chunkRun{firstChunk: binary.BigEndian.Uint32(entry[:4]), samplesPerChunk: binary.BigEndian.Uint32(entry[4:8]), descriptionIndex: binary.BigEndian.Uint32(entry[8:12])}
		if current.firstChunk == 0 || current.samplesPerChunk == 0 || current.descriptionIndex == 0 || current.descriptionIndex > descriptions || current.firstChunk > chunks {
			return fmt.Errorf("%w: stsc entry %d", errMalformedMovie, index+1)
		}
		if index == 0 {
			if current.firstChunk != 1 {
				return fmt.Errorf("%w: first stsc chunk is not one", errMalformedMovie)
			}
		} else {
			if current.firstChunk <= previous.firstChunk {
				return fmt.Errorf("%w: stsc first_chunk is not ascending", errMalformedMovie)
			}
			if err := addChunkSamples(&described, uint64(current.firstChunk-previous.firstChunk), previous.samplesPerChunk); err != nil {
				return err
			}
		}
		previous = current
	}
	if samples == 0 {
		if chunks != 0 || entryCount != 0 {
			return fmt.Errorf("%w: empty track has chunks", errMalformedMovie)
		}
		return nil
	}
	if err := addChunkSamples(&described, uint64(chunks-previous.firstChunk+1), previous.samplesPerChunk); err != nil {
		return err
	}
	if described != samples {
		return fmt.Errorf("%w: stsc and sample counts disagree", errMalformedMovie)
	}
	return nil
}

func addChunkSamples(total *uint64, chunks uint64, samplesPerChunk uint32) error {
	product := chunks * uint64(samplesPerChunk)
	if chunks != 0 && product/chunks != uint64(samplesPerChunk) {
		return fmt.Errorf("%w: stsc sample count overflows", errMalformedMovie)
	}
	next, ok := checkedBoxAdd(*total, product)
	if !ok {
		return fmt.Errorf("%w: stsc sample count overflows", errMalformedMovie)
	}
	*total = next
	return nil
}

func scanSyncSamples(ctx context.Context, reader access.Random, value box, samples uint64) error {
	entries, err := newTableReader(ctx, reader, value, "stss", 4, 0, false)
	if err != nil {
		return err
	}
	var previous uint32
	for entries.remaining > 0 {
		entry, err := entries.next(ctx)
		if err != nil {
			return err
		}
		number := binary.BigEndian.Uint32(entry[:4])
		if number == 0 || number <= previous || uint64(number) > samples {
			return fmt.Errorf("%w: stss sample number", errMalformedMovie)
		}
		previous = number
	}
	return nil
}

type chunkRun struct {
	firstChunk       uint32
	samplesPerChunk  uint32
	descriptionIndex uint32
}
