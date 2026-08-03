// Package audio defines the typed planar sample frame used by the first
// foundation consumer.
package audio

import (
	"errors"
	"math"
	"unsafe"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

type Sample interface {
	~int8 | ~int16 | ~int32 | ~int64 |
		~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

var (
	ErrInvalidSampleCount = errors.New("audio sample count must be non-negative")
	ErrInvalidPlanes      = errors.New("audio frame planes do not match sample count")
	ErrSampleSizeOverflow = errors.New("audio frame sample size overflow")
)

// Frame is a typed, planar sample frame. Sample rate, channel layout, and
// valid bits belong to stream.Descriptor properties and are not repeated here.
type Frame[S Sample] struct {
	pts     timing.OptionalPTS
	samples int
	planes  buffer.Handle
}

func NewFrame[S Sample](pts timing.OptionalPTS, samples int, planes buffer.Handle) (Frame[S], error) {
	if samples < 0 {
		return Frame[S]{}, ErrInvalidSampleCount
	}
	layout := planes.Layout()
	if len(layout.Planes) == 0 {
		return Frame[S]{}, ErrInvalidPlanes
	}
	sampleSize := int(unsafe.Sizeof(*new(S)))
	if sampleSize <= 0 || samples > math.MaxInt/sampleSize {
		return Frame[S]{}, ErrSampleSizeOverflow
	}
	required := samples * sampleSize
	for _, plane := range layout.Planes {
		if plane.Size < required {
			return Frame[S]{}, ErrInvalidPlanes
		}
	}
	return Frame[S]{pts: pts, samples: samples, planes: planes}, nil
}

func (f Frame[S]) Valid() bool             { return f.planes.Valid() }
func (f Frame[S]) PTS() timing.OptionalPTS { return f.pts }
func (f Frame[S]) Samples() int            { return f.samples }

// Planes returns a borrowed view valid until the frame owner is released.
// Call View.Share when the planes must outlive this frame.
func (f Frame[S]) Planes() buffer.View             { return f.planes.Borrow() }
func (f Frame[S]) Plane(index int) ([]byte, error) { return f.planes.Borrow().Plane(index) }
func (f Frame[S]) Share() Frame[S] {
	f.planes = f.planes.Share()
	return f
}
func (f Frame[S]) Release() { f.planes.Release() }
