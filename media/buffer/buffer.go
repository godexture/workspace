// Package buffer provides aligned backing storage and ownership handles.
package buffer

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"
)

var (
	ErrInvalidAlignment = errors.New("buffer alignment must be a positive power of two")
	ErrInvalidPlane     = errors.New("buffer plane size and padding must be non-negative")
	ErrReadOnly         = errors.New("buffer is read-only")
	ErrPlaneIndex       = errors.New("buffer plane index is out of range")
)

type PlaneSpec struct {
	Size    int
	Padding int
}

type Plane struct {
	Offset  int
	Size    int
	Padding int
}

type Layout struct {
	Alignment int
	Size      int
	Planes    []Plane
	ReadOnly  bool
	Shared    bool
}

func (l Layout) Clone() Layout {
	return Layout{Alignment: l.Alignment, Size: l.Size, Planes: append([]Plane(nil), l.Planes...), ReadOnly: l.ReadOnly, Shared: l.Shared}
}

type Spec struct {
	Planes    []PlaneSpec
	Alignment int
	ReadOnly  bool
	Shared    bool
}

type storage struct {
	refs   atomic.Int64
	data   []byte
	layout Layout
}

type lease struct {
	storage  *storage
	released atomic.Bool
}

// Handle is an owned reference to one aligned backing allocation. Share must
// be used before creating another owner.
type Handle struct{ lease *lease }

// View is a borrowed read-only view. It does not retain the backing storage;
// the owner must outlive every use of the view. Call Share when a retained
// Handle is needed.
type View struct{ storage *storage }

func Allocate(spec Spec) (Handle, error) {
	alignment := spec.Alignment
	if alignment == 0 {
		alignment = 1
	}
	if alignment < 1 || alignment&(alignment-1) != 0 {
		return Handle{}, ErrInvalidAlignment
	}
	planes := make([]Plane, len(spec.Planes))
	position := 0
	for index, plane := range spec.Planes {
		if plane.Size < 0 || plane.Padding < 0 {
			return Handle{}, fmt.Errorf("plane %d: %w", index, ErrInvalidPlane)
		}
		position = align(position, alignment)
		planes[index] = Plane{Offset: position, Size: plane.Size, Padding: plane.Padding}
		position += plane.Size + plane.Padding
		if position < 0 {
			return Handle{}, errors.New("buffer size overflow")
		}
	}
	rawSize := position
	if rawSize == 0 {
		rawSize = 1
	}
	raw := make([]byte, rawSize+alignment-1)
	base := uintptr(unsafe.Pointer(&raw[0]))
	offset := int((uintptr(alignment) - base%uintptr(alignment)) % uintptr(alignment))
	data := raw[offset : offset+position : offset+position]
	state := &storage{data: data, layout: Layout{Alignment: alignment, Size: position, Planes: planes, ReadOnly: spec.ReadOnly, Shared: spec.Shared}}
	state.refs.Store(1)
	return Handle{lease: &lease{storage: state}}, nil
}

func FromBytes(value []byte, alignment int) (Handle, error) {
	result, err := Allocate(Spec{Alignment: alignment, Planes: []PlaneSpec{{Size: len(value)}}})
	if err != nil {
		return Handle{}, err
	}
	copy(result.lease.storage.data, value)
	return result, nil
}

func align(value, alignment int) int {
	mask := alignment - 1
	return (value + mask) &^ mask
}

func (h Handle) Valid() bool {
	return h.lease != nil && h.lease.storage != nil && h.lease.storage.refs.Load() > 0 && !h.lease.released.Load()
}

func (h Handle) Borrow() View {
	if h.lease == nil {
		return View{}
	}
	return View{storage: h.lease.storage}
}

func (h Handle) Share() Handle {
	if h.lease == nil || !h.lease.storage.retain() {
		return Handle{}
	}
	return Handle{lease: &lease{storage: h.lease.storage}}
}

func (h Handle) Layout() Layout { return h.Borrow().Layout() }

func (h Handle) Bytes() []byte { return h.Borrow().Bytes() }

func (h Handle) MutableBytes() ([]byte, error) {
	if !h.Valid() {
		return nil, errors.New("invalid buffer handle")
	}
	if h.lease.storage.layout.ReadOnly {
		return nil, ErrReadOnly
	}
	return h.lease.storage.data, nil
}

func (h Handle) Plane(index int) ([]byte, error) { return h.Borrow().Plane(index) }

func (h Handle) ReadOnly() bool {
	return h.lease != nil && h.lease.storage != nil && h.lease.storage.layout.ReadOnly
}

func (h Handle) Shared() bool {
	return h.lease != nil && h.lease.storage != nil && h.lease.storage.layout.Shared
}

func (h Handle) Release() {
	if h.lease == nil || !h.lease.released.CompareAndSwap(false, true) {
		return
	}
	if h.lease.storage.refs.Add(-1) == 0 {
		h.lease.storage.data = nil
	}
}

func (v View) Valid() bool {
	return v.storage != nil && v.storage.refs.Load() > 0
}

func (v View) Share() Handle {
	if !v.Valid() || !v.storage.retain() {
		return Handle{}
	}
	return Handle{lease: &lease{storage: v.storage}}
}

func (v View) Layout() Layout {
	if !v.Valid() {
		return Layout{}
	}
	return v.storage.layout.Clone()
}

func (v View) Bytes() []byte {
	if !v.Valid() {
		return nil
	}
	return v.storage.data
}

func (v View) Plane(index int) ([]byte, error) {
	if !v.Valid() {
		return nil, errors.New("invalid buffer view")
	}
	planes := v.storage.layout.Planes
	if index < 0 || index >= len(planes) {
		return nil, ErrPlaneIndex
	}
	plane := planes[index]
	return v.storage.data[plane.Offset : plane.Offset+plane.Size : plane.Offset+plane.Size], nil
}

func (v View) ReadOnly() bool {
	return v.Valid() && v.storage.layout.ReadOnly
}

func (v View) Shared() bool {
	return v.Valid() && v.storage.layout.Shared
}

func (state *storage) retain() bool {
	if state == nil {
		return false
	}
	for {
		refs := state.refs.Load()
		if refs == 0 {
			return false
		}
		if state.refs.CompareAndSwap(refs, refs+1) {
			return true
		}
	}
}
