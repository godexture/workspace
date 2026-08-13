// Package audio defines the typed planar sample frame used by the first
// foundation consumer.
package audio

import (
	"errors"
	"math"
	"unsafe"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

type Sample interface {
	~int8 | ~int16 | ~int32 | ~int64 |
		~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Samples is an immutable borrowed sample view. Mutable sample slices are
// available only from Editor.
type Samples[S Sample] struct {
	data buffer.Bytes
	len  int
}

func (s Samples[S]) Valid() bool { return s.data.Valid() }

// Len reports the recorded sample count and does not revalidate the
// originating frame, so it stays cheap in loop conditions. Every read still
// fails or panics once that frame is released; use Valid to test liveness.
func (s Samples[S]) Len() int { return s.len }

// At reads one sample and revalidates the originating frame on every call, so
// it suits incidental access only. Read ranges through CopyTo or AppendTo, or
// drain the byte plane with buffer.Bytes.Blocks.
func (s Samples[S]) At(index int) S {
	if index < 0 || index >= s.len {
		panic("audio sample index out of range")
	}
	var value S
	size := int(unsafe.Sizeof(value))
	view, err := s.data.Slice(index*size, size)
	if err != nil {
		panic("audio samples outlived their originating frame")
	}
	view.CopyTo(unsafe.Slice((*byte)(unsafe.Pointer(&value)), size))
	return value
}

func (s Samples[S]) CopyTo(destination []S) int {
	count := min(len(destination), s.len)
	if count == 0 {
		return 0
	}
	size := int(unsafe.Sizeof(*new(S)))
	data, err := s.data.Slice(0, count*size)
	if err != nil {
		return 0
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&destination[0])), count*size)
	return data.CopyTo(bytes) / size
}

func (s Samples[S]) AppendTo(destination []S) []S {
	if !s.data.Valid() {
		return destination
	}
	start := len(destination)
	destination = append(destination, make([]S, s.len)...)
	s.CopyTo(destination[start:])
	return destination
}

var (
	ErrInvalidSampleCount = errors.New("audio sample count must be non-negative")
	ErrInvalidPlanes      = errors.New("audio frame planes do not match sample count")
	ErrSampleAlignment    = errors.New("audio frame plane is not aligned for its sample type")
	ErrSampleSizeOverflow = errors.New("audio frame sample size overflow")
)

// Frame is a typed, planar sample frame. Sample rate, channel layout, and
// valid bits belong to stream.Descriptor properties and are not repeated here.
type Frame[S Sample] struct {
	pts      timing.OptionalPTS
	samples  int
	planes   buffer.Handle
	sideData side.Data
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
	for index, plane := range layout.Planes {
		if plane.Size < required {
			return Frame[S]{}, ErrInvalidPlanes
		}
		if samples != 0 {
			plane, err := planes.Plane(index)
			if err != nil || plane.Len() < required {
				return Frame[S]{}, ErrInvalidPlanes
			}
			aligned, alignErr := planes.PlaneAligned(index, int(unsafe.Alignof(*new(S))))
			if alignErr != nil || !aligned {
				return Frame[S]{}, ErrSampleAlignment
			}
		}
	}
	return Frame[S]{pts: pts, samples: samples, planes: planes}, nil
}

func (f Frame[S]) Valid() bool             { return f.planes.Valid() }
func (f Frame[S]) PTS() timing.OptionalPTS { return f.pts }
func (f Frame[S]) Samples() int            { return f.samples }
func (f Frame[S]) SideData() side.Data     { return f.sideData }

// WithSideData returns a copy carrying immutable side data.
func (f Frame[S]) WithSideData(value side.Data) Frame[S] { f.sideData = value; return f }

// Planes returns a borrowed view valid until the frame owner is released.
// Call View.Share when the planes must outlive this frame.
func (f Frame[S]) Planes() buffer.View                   { return f.planes.Borrow() }
func (f Frame[S]) Plane(index int) (buffer.Bytes, error) { return f.planes.Borrow().Plane(index) }

// PlaneSamples returns a borrowed typed plane valid until the frame owner is
// released. The constructor has already checked length and scalar alignment.
func (f Frame[S]) PlaneSamples(index int) (Samples[S], error) {
	plane, err := f.Plane(index)
	if err != nil {
		return Samples[S]{}, err
	}
	return Samples[S]{data: plane, len: f.samples}, nil
}

func (f Frame[S]) Share() Frame[S] {
	f.planes = f.planes.Share()
	return f
}
func (f Frame[S]) Release() { f.planes.Release() }

// Editor provides transactional mutable access to a frame. Frame returns the
// candidate owner to emit downstream. Call Commit only after a successful
// emit; defer Discard so a failed path releases only a copy-on-write candidate.
type Editor[S Sample] struct {
	frame Frame[S]
	edit  buffer.Edit
}

func (f Frame[S]) Edit(allocator *buffer.Allocator) (Editor[S], error) {
	edit, err := f.planes.Edit(allocator)
	if err != nil {
		return Editor[S]{}, err
	}
	f.planes = edit.Handle()
	return Editor[S]{frame: f, edit: edit}, nil
}

func (e *Editor[S]) Frame() Frame[S] {
	if e == nil || !e.edit.Handle().Valid() {
		return Frame[S]{}
	}
	return e.frame
}

func (e *Editor[S]) PlaneSamples(index int) ([]S, error) {
	if e == nil {
		return nil, buffer.ErrLeaseState
	}
	plane, err := e.edit.MutablePlane(index)
	if err != nil {
		return nil, err
	}
	if e.frame.samples == 0 {
		return []S{}, nil
	}
	return unsafe.Slice((*S)(unsafe.Pointer(&plane[0])), e.frame.samples), nil
}

func (e *Editor[S]) Copied() bool { return e != nil && e.edit.Copied() }
func (e *Editor[S]) Commit() error {
	if e == nil {
		return buffer.ErrLeaseState
	}
	return e.edit.Commit()
}
func (e *Editor[S]) Discard() {
	if e != nil {
		e.edit.Discard()
	}
}
