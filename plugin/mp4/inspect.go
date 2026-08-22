package mp4

import (
	"context"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/resource"
)

func inspectMP4(ctx mediaformat.InspectContext) (mediaformat.Inspection, error) {
	random, ok := access.RandomOf(ctx.Opening())
	if !ok {
		return mediaformat.Inspection{}, fmt.Errorf("%w: MP4 Inspect requires random read", ErrUnsupported)
	}
	sizer, ok := access.StableSizeOf(ctx.Opening())
	if !ok {
		return mediaformat.Inspection{}, fmt.Errorf("%w: MP4 Inspect requires stable size", ErrUnsupported)
	}
	size, err := sizer.Size(ctx.Context())
	if err != nil {
		return mediaformat.Inspection{}, err
	}
	if size <= 0 {
		return mediaformat.Inspection{}, fmt.Errorf("%w: stable source size is %d", ErrMalformed, size)
	}
	value, err := parseMovie(ctx.Context(), random, uint64(size), ctx.Limit(), ctx.MemoryLimit())
	if err != nil {
		return mediaformat.Inspection{}, err
	}
	if err := validateInspectedMovie(value); err != nil {
		return mediaformat.Inspection{}, err
	}
	return mediaformat.NewInspection(MP4(), value), nil
}

// inspectReader charges every parser ReadAt before the underlying source sees
// it. Cursors are intentionally constructed with the unwrapped borrowed
// source, because Open is outside Inspect's resource budget.
type inspectReader struct {
	reader    access.Random
	remaining uint64
}

func newInspectReader(reader access.Random, limit resource.Bytes) *inspectReader {
	return &inspectReader{reader: reader, remaining: uint64(limit)}
}

func (r *inspectReader) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if uint64(len(destination)) > r.remaining {
		return 0, fmt.Errorf("%w: inspect read limit needs %d bytes with %d remaining", errUnsupportedMovie, len(destination), r.remaining)
	}
	r.remaining -= uint64(len(destination))
	if offset < 0 || len(destination) > 0 && offset > math.MaxInt64-int64(len(destination)-1) {
		return 0, access.ErrInvalidRead
	}
	return r.reader.ReadAt(ctx, destination, offset)
}
