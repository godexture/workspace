package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

// sampleCursor reconstructs one track's samples from frozen table ranges. The
// cursor owns the borrowed reader; movie deliberately never does.
type sampleCursor struct {
	reader access.Random
	track  track

	sequence uint64
	dts      uint64

	timing        rawTableReader
	timingLeft    uint32
	timingValue   uint32
	composition   rawTableReader
	composeLeft   uint32
	composeValue  int64
	composeSigned bool

	layout        rawTableReader
	layoutCurrent chunkRun
	layoutNext    chunkRun
	haveLayout    bool
	haveNext      bool
	chunk         uint32
	chunkLeft     uint32
	chunkOffset   uint64

	offsets rawTableReader
	sizes   sizeCursor
	sync    rawTableReader
	syncAt  uint32
	hasSync bool
}

func newSampleCursor(ctx context.Context, reader access.Random, value track) (sampleCursor, error) {
	if reader == nil {
		return sampleCursor{}, fmt.Errorf("%w: sample cursor has no reader", errMalformedMovie)
	}
	timing, err := newTableReader(ctx, reader, value.tables.timing, "stts", 8, 0, false)
	if err != nil {
		return sampleCursor{}, err
	}
	layout, err := newTableReader(ctx, reader, value.tables.layout, "stsc", 12, 0, false)
	if err != nil {
		return sampleCursor{}, err
	}
	offsetSize := uint64(4)
	offsetName := "stco"
	if value.tables.largeOffsets {
		offsetSize = 8
		offsetName = "co64"
	}
	offsets, err := newTableReader(ctx, reader, value.tables.offsets, offsetName, offsetSize, 0, false)
	if err != nil {
		return sampleCursor{}, err
	}
	sizes, err := newSizeCursor(ctx, reader, value.tables.sizes, value.tables.compactSizes)
	if err != nil {
		return sampleCursor{}, err
	}
	result := sampleCursor{reader: reader, track: value, timing: timing, layout: layout, offsets: offsets, sizes: sizes, hasSync: value.tables.hasSync}
	if value.tables.hasComposition {
		composition, err := newTableReader(ctx, reader, value.tables.composition, "ctts", 8, 0, true)
		if err != nil {
			return sampleCursor{}, err
		}
		result.composition = composition
		var version [1]byte
		if err := readBoxPrefix(ctx, reader, value.tables.composition, version[:], "ctts"); err != nil {
			return sampleCursor{}, err
		}
		result.composeSigned = version[0] == 1
	}
	if value.tables.hasSync {
		sync, err := newTableReader(ctx, reader, value.tables.sync, "stss", 4, 0, false)
		if err != nil {
			return sampleCursor{}, err
		}
		result.sync = sync
		if err := result.loadSync(ctx); err != nil {
			return sampleCursor{}, err
		}
	}
	if value.sampleCount != 0 {
		if err := result.loadLayout(ctx); err != nil {
			return sampleCursor{}, err
		}
	}
	return result, nil
}

func (c *sampleCursor) next(ctx context.Context) (sample, bool, error) {
	if c.sequence == c.track.sampleCount {
		return sample{}, false, nil
	}
	if c.sequence == math.MaxUint64 {
		return sample{}, false, fmt.Errorf("%w: sample sequence overflows", errMalformedMovie)
	}
	if c.chunkLeft == 0 {
		if err := c.nextChunk(ctx); err != nil {
			return sample{}, false, err
		}
	}
	duration, err := c.nextTiming(ctx)
	if err != nil {
		return sample{}, false, err
	}
	composition, err := c.nextComposition(ctx)
	if err != nil {
		return sample{}, false, err
	}
	size, err := c.sizes.next(ctx)
	if err != nil {
		return sample{}, false, err
	}
	if c.dts > math.MaxInt64 {
		return sample{}, false, fmt.Errorf("%w: PTS exceeds runtime range", errUnsupportedMovie)
	}
	pts, ok := addCompositionOffset(c.dts, composition)
	if !ok {
		return sample{}, false, fmt.Errorf("%w: PTS overflows", errMalformedMovie)
	}
	c.sequence++
	item := sample{
		offset:           c.chunkOffset,
		size:             size,
		duration:         duration,
		dts:              c.dts,
		pts:              pts,
		descriptionIndex: c.layoutCurrent.descriptionIndex,
		sequence:         c.sequence,
	}
	if !c.hasSync {
		item.sync = true
	} else if c.syncAt != 0 && uint64(c.syncAt) == c.sequence {
		item.sync = true
		if err := c.loadSync(ctx); err != nil {
			return sample{}, false, err
		}
	}
	nextOffset, ok := checkedBoxAdd(c.chunkOffset, uint64(size))
	if !ok {
		return sample{}, false, fmt.Errorf("%w: sample offset overflows", errMalformedMovie)
	}
	c.chunkOffset = nextOffset
	c.chunkLeft--
	nextDTS, ok := checkedBoxAdd(c.dts, uint64(duration))
	if !ok {
		return sample{}, false, fmt.Errorf("%w: DTS overflows", errMalformedMovie)
	}
	c.dts = nextDTS
	return item, true, nil
}

func (c *sampleCursor) nextTiming(ctx context.Context) (uint32, error) {
	if c.timingLeft == 0 {
		entry, err := c.timing.next(ctx)
		if err != nil {
			return 0, err
		}
		c.timingLeft = binary.BigEndian.Uint32(entry[:4])
		c.timingValue = binary.BigEndian.Uint32(entry[4:8])
		if c.timingLeft == 0 {
			return 0, fmt.Errorf("%w: stts entry has zero samples", errMalformedMovie)
		}
	}
	c.timingLeft--
	return c.timingValue, nil
}

func (c *sampleCursor) nextComposition(ctx context.Context) (int64, error) {
	if !c.track.tables.hasComposition {
		return 0, nil
	}
	if c.composeLeft == 0 {
		entry, err := c.composition.next(ctx)
		if err != nil {
			return 0, err
		}
		c.composeLeft = binary.BigEndian.Uint32(entry[:4])
		if c.composeLeft == 0 {
			return 0, fmt.Errorf("%w: ctts entry has zero samples", errMalformedMovie)
		}
		offset := binary.BigEndian.Uint32(entry[4:8])
		if c.composeSigned {
			c.composeValue = int64(int32(offset))
		} else {
			c.composeValue = int64(offset)
		}
	}
	c.composeLeft--
	return c.composeValue, nil
}

func (c *sampleCursor) loadLayout(ctx context.Context) error {
	entry, err := c.layout.next(ctx)
	if err != nil {
		return err
	}
	c.layoutCurrent = chunkRun{firstChunk: binary.BigEndian.Uint32(entry[:4]), samplesPerChunk: binary.BigEndian.Uint32(entry[4:8]), descriptionIndex: binary.BigEndian.Uint32(entry[8:12])}
	c.haveLayout = true
	return c.loadNextLayout(ctx)
}

func (c *sampleCursor) loadNextLayout(ctx context.Context) error {
	c.haveNext = false
	if c.layout.remaining == 0 {
		return nil
	}
	entry, err := c.layout.next(ctx)
	if err != nil {
		return err
	}
	c.layoutNext = chunkRun{firstChunk: binary.BigEndian.Uint32(entry[:4]), samplesPerChunk: binary.BigEndian.Uint32(entry[4:8]), descriptionIndex: binary.BigEndian.Uint32(entry[8:12])}
	c.haveNext = true
	return nil
}

func (c *sampleCursor) nextChunk(ctx context.Context) error {
	if !c.haveLayout || c.chunk == math.MaxUint32 {
		return fmt.Errorf("%w: stsc chunk layout", errMalformedMovie)
	}
	c.chunk++
	if c.haveNext && c.layoutNext.firstChunk == c.chunk {
		c.layoutCurrent = c.layoutNext
		if err := c.loadNextLayout(ctx); err != nil {
			return err
		}
	}
	if c.haveNext && c.layoutNext.firstChunk < c.chunk {
		return fmt.Errorf("%w: stsc chunk layout", errMalformedMovie)
	}
	entry, err := c.offsets.next(ctx)
	if err != nil {
		return err
	}
	if c.track.tables.largeOffsets {
		c.chunkOffset = binary.BigEndian.Uint64(entry[:8])
	} else {
		c.chunkOffset = uint64(binary.BigEndian.Uint32(entry[:4]))
	}
	c.chunkLeft = c.layoutCurrent.samplesPerChunk
	if c.chunkLeft == 0 {
		return fmt.Errorf("%w: stsc chunk layout", errMalformedMovie)
	}
	return nil
}

func (c *sampleCursor) loadSync(ctx context.Context) error {
	c.syncAt = 0
	if c.sync.remaining == 0 {
		return nil
	}
	entry, err := c.sync.next(ctx)
	if err != nil {
		return err
	}
	c.syncAt = binary.BigEndian.Uint32(entry[:4])
	return nil
}
