package mp4

import (
	"context"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/resource"
)

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
