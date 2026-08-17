package mp4

import (
	"context"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

func validateTrack(ctx context.Context, reader access.Random, media box, value *track) error {
	cursor, err := newSampleCursor(ctx, reader, *value)
	if err != nil {
		return err
	}
	for {
		item, more, err := cursor.next(ctx)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		if !withinMedia(media, item.offset, item.size) {
			return fmt.Errorf("%w: sample %d lies outside mdat payload", errMalformedMovie, item.sequence)
		}
	}
}

func withinMedia(media box, offset uint64, size uint32) bool {
	end, ok := checkedBoxAdd(offset, uint64(size))
	if !ok {
		return false
	}
	mediaEnd, ok := payloadEnd(media)
	return ok && offset >= media.payloadOffset && end <= mediaEnd
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
