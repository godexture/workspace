package access

import (
	"context"
	"errors"
	"io"
)

var ErrInvalidProbeRange = errors.New("access probe range is invalid")

// RangeRequest asks the host for one additional bounded byte range.
type RangeRequest struct {
	offset int64
	length int64
}

func NewRangeRequest(offset, length int64) (RangeRequest, error) {
	if offset < 0 || length < 0 {
		return RangeRequest{}, ErrInvalidProbeRange
	}
	return RangeRequest{offset: offset, length: length}, nil
}

func (r RangeRequest) Valid() bool   { return r.offset >= 0 && r.length >= 0 }
func (r RangeRequest) Offset() int64 { return r.offset }
func (r RangeRequest) Length() int64 { return r.length }

// A probe receives the Random view and nothing else, which is what keeps it
// from moving a shared cursor.
var _ Random = ProbeView{}

// ProbeView is an immutable, cursor-independent view over one bounded source
// range. New views copy the supplied bytes so a probe cannot observe producer
// mutation.
type ProbeView struct {
	valid bool
	base  int64
	data  []byte
}

func NewProbeView(data []byte) ProbeView {
	view, _ := NewProbeViewAt(0, data)
	return view
}

func NewProbeViewAt(base int64, data []byte) (ProbeView, error) {
	if base < 0 {
		return ProbeView{}, ErrInvalidProbeRange
	}
	return ProbeView{valid: true, base: base, data: append([]byte(nil), data...)}, nil
}

func (v ProbeView) Valid() bool { return v.valid }
func (v ProbeView) Base() int64 { return v.base }
func (v ProbeView) Size() int64 { return int64(len(v.data)) }

// Bytes returns a copy of the bounded bytes.
func (v ProbeView) Bytes() []byte { return append([]byte(nil), v.data...) }

// Range returns the source range represented by this view.
func (v ProbeView) Range() RangeRequest {
	return RangeRequest{offset: v.base, length: int64(len(v.data))}
}

// ReadAt implements the position-independent access contract. Offset is an
// absolute source offset, so reading never changes the view or a shared
// cursor.
func (v ProbeView) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
	}
	if offset < v.base || offset > v.base+int64(len(v.data)) {
		return 0, ErrInvalidProbeRange
	}
	if len(destination) == 0 {
		return 0, nil
	}
	if offset == v.base+int64(len(v.data)) {
		return 0, io.EOF
	}
	n := copy(destination, v.data[offset-v.base:])
	if n != len(destination) {
		return n, io.EOF
	}
	return n, nil
}
