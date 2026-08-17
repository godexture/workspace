package access

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/godexture/godec/media/buffer"
)

var ErrInvalidProbeRange = errors.New("access probe range is invalid")

// RangeRequest asks the host for one additional bounded byte range.
type RangeRequest struct {
	offset int64
	length int64
}

func NewRangeRequest(offset, length int64) (RangeRequest, error) {
	if offset < 0 || length <= 0 || offset > math.MaxInt64-length {
		return RangeRequest{}, ErrInvalidProbeRange
	}
	return RangeRequest{offset: offset, length: length}, nil
}

func (r RangeRequest) Valid() bool {
	return r.offset >= 0 && r.length > 0 && r.offset <= math.MaxInt64-r.length
}
func (r RangeRequest) Offset() int64 { return r.offset }
func (r RangeRequest) Length() int64 { return r.length }
func (r RangeRequest) End() int64 {
	if !r.Valid() {
		return 0
	}
	return r.offset + r.length
}

// A probe receives the Random view and nothing else, which is what keeps it
// from moving a shared cursor.
var _ Random = ProbeView{}

// ProbeView is an immutable, cursor-independent view over one bounded source
// range. New views copy the supplied bytes so a probe cannot observe producer
// mutation.
type ProbeView struct {
	valid    bool
	buffered bool
	base     int64
	data     []byte
	view     buffer.View
}

func NewProbeView(data []byte) ProbeView {
	view, _ := NewProbeViewAt(0, data)
	return view
}

func NewProbeViewAt(base int64, data []byte) (ProbeView, error) {
	if base < 0 || uint64(len(data)) > math.MaxInt64 || base > math.MaxInt64-int64(len(data)) {
		return ProbeView{}, ErrInvalidProbeRange
	}
	return ProbeView{valid: true, base: base, data: append([]byte(nil), data...)}, nil
}

// NewProbeViewFromBuffer borrows one grant-backed read-only buffer. The
// buffer owner must retain it for every Probe call that receives the view.
func NewProbeViewFromBuffer(base int64, value buffer.View) (ProbeView, error) {
	layout := value.Layout()
	if base < 0 || !value.Valid() || !value.ReadOnly() || len(layout.Planes) != 1 || layout.Planes[0].Size != layout.Size || base > math.MaxInt64-int64(layout.Size) {
		return ProbeView{}, ErrInvalidProbeRange
	}
	return ProbeView{valid: true, buffered: true, base: base, view: value}, nil
}

func (v ProbeView) Valid() bool {
	return v.valid && (!v.buffered || v.view.Valid())
}
func (v ProbeView) Base() int64 { return v.base }
func (v ProbeView) Size() int64 {
	if v.buffered {
		return int64(v.view.Bytes().Len())
	}
	return int64(len(v.data))
}

// Bytes returns a copy of the bounded bytes.
func (v ProbeView) Bytes() []byte {
	if v.buffered {
		return v.view.Bytes().AppendTo(nil)
	}
	return append([]byte(nil), v.data...)
}

// Range returns the source range represented by this view.
func (v ProbeView) Range() RangeRequest {
	return RangeRequest{offset: v.base, length: v.Size()}
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
	if !v.Valid() {
		return 0, ErrInvalidProbeRange
	}
	size := v.Size()
	if offset < v.base || offset > v.base+size {
		return 0, ErrInvalidProbeRange
	}
	if len(destination) == 0 {
		return 0, nil
	}
	if offset == v.base+size {
		return 0, io.EOF
	}
	available := int(size - (offset - v.base))
	count := min(len(destination), available)
	var n int
	if v.buffered {
		data := v.view.Bytes()
		part, err := data.Slice(int(offset-v.base), count)
		if err != nil {
			return 0, ErrInvalidProbeRange
		}
		n = part.CopyTo(destination)
	} else {
		n = copy(destination, v.data[offset-v.base:])
	}
	if n != len(destination) {
		return n, io.EOF
	}
	return n, nil
}
