package audio

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

// Record describes a Block's shape (timing, sample count, and metadata)
// without retaining its sample data. Spool uses it to index buffered audio;
// callers that track block shape without buffering samples at all (e.g. a
// silence-run replay buffer) can reuse the same bookkeeping.
type Record struct {
	PTS      media.Pts
	Samples  int
	Metadata *metadata.Bundle
}
