package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

const tablePageBytes = 4_080

// rawTableReader reads one fixed-width sample-table entry at a time from a
// fixed page. tablePageBytes is divisible by every supported entry width.
type rawTableReader struct {
	reader    access.Random
	offset    uint64
	remaining uint32
	entrySize uint64
	what      string
	page      [tablePageBytes]byte
	pageUsed  uint64
	pageNext  uint64
}

// tableRangeReader caches variable-width records such as stsd sample entries.
type tableRangeReader struct {
	reader access.Random
	end    uint64
	start  uint64
	used   uint64
	page   [tablePageBytes]byte
}

func newTableRangeReader(reader access.Random, end uint64) tableRangeReader {
	return tableRangeReader{reader: reader, end: end, start: end}
}

func (r *tableRangeReader) readAt(ctx context.Context, offset uint64, destination []byte, what string) error {
	length := uint64(len(destination))
	end, ok := checkedBoxAdd(offset, length)
	if !ok || end > r.end {
		return fmt.Errorf("%w: %s entry range", errMalformedMovie, what)
	}
	cacheEnd, cacheOK := checkedBoxAdd(r.start, r.used)
	if !cacheOK || offset < r.start || end > cacheEnd {
		available := r.end - offset
		if available > tablePageBytes {
			available = tablePageBytes
		}
		if err := readMovieAt(ctx, r.reader, r.page[:available], offset, what); err != nil {
			return err
		}
		r.start = offset
		r.used = available
	}
	copy(destination, r.page[offset-r.start:end-r.start])
	return nil
}

func newTableReader(ctx context.Context, reader access.Random, value box, what string, entrySize uint64, expectedVersion byte, allowVersionOne bool) (rawTableReader, error) {
	if value.payloadSize < 8 {
		return rawTableReader{}, fmt.Errorf("%w: %s has no entry count", errMalformedMovie, what)
	}
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], what); err != nil {
		return rawTableReader{}, err
	}
	version, flags, err := fullBox(prefix[:4], what)
	if err != nil {
		return rawTableReader{}, err
	}
	if flags != 0 || version != expectedVersion && !(allowVersionOne && version == 1) {
		return rawTableReader{}, fmt.Errorf("%w: %s full-box header", errUnsupportedMovie, what)
	}
	count := binary.BigEndian.Uint32(prefix[4:])
	bytes := uint64(count) * entrySize
	if count != 0 && bytes/uint64(count) != entrySize || bytes > math.MaxUint64-8 || value.payloadSize != 8+bytes {
		return rawTableReader{}, fmt.Errorf("%w: %s entries", errMalformedMovie, what)
	}
	offset, ok := checkedBoxAdd(value.payloadOffset, 8)
	if !ok {
		return rawTableReader{}, fmt.Errorf("%w: %s range", errMalformedMovie, what)
	}
	return rawTableReader{reader: reader, offset: offset, remaining: count, entrySize: entrySize, what: what}, nil
}

func (r *rawTableReader) next(ctx context.Context) ([]byte, error) {
	if r.remaining == 0 {
		return nil, fmt.Errorf("%w: %s has no more entries", errMalformedMovie, r.what)
	}
	if r.pageNext == r.pageUsed {
		bytes := uint64(r.remaining) * r.entrySize
		if bytes > tablePageBytes {
			bytes = tablePageBytes
		}
		if err := readMovieAt(ctx, r.reader, r.page[:bytes], r.offset, r.what); err != nil {
			return nil, err
		}
		r.pageUsed = bytes
		r.pageNext = 0
	}
	end := r.pageNext + r.entrySize
	if end > r.pageUsed {
		return nil, fmt.Errorf("%w: %s page alignment", errMalformedMovie, r.what)
	}
	entry := r.page[r.pageNext:end]
	r.pageNext = end
	next, ok := checkedBoxAdd(r.offset, r.entrySize)
	if !ok {
		return nil, fmt.Errorf("%w: %s entry range", errMalformedMovie, r.what)
	}
	r.offset = next
	r.remaining--
	return entry, nil
}
