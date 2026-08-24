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
	ErrInvalidLimit     = errors.New("buffer allocator limit must be positive")
	ErrLimit            = errors.New("buffer allocator grant is exhausted")
	ErrReadOnly         = errors.New("buffer is read-only")
	ErrShared           = errors.New("buffer is not exclusively writable")
	ErrPlaneIndex       = errors.New("buffer plane index is out of range")
	ErrLeaseState       = errors.New("buffer overwrite lease is not writable")
	ErrInvalidHandle    = errors.New("buffer handle is invalid")
	ErrRange            = errors.New("buffer range is out of bounds")
	ErrEditAllocator    = errors.New("copy-on-write requires a payload allocator")
	ErrLayoutOverflow   = errors.New("buffer layout size overflows the address space")
)

const maxInt = int(^uint(0) >> 1)

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
	charge int64
	repay  func(int64)
}

type lease struct {
	storage  *storage
	offset   int
	layout   Layout
	released atomic.Bool
}

// Handle is an owned reference to one aligned backing allocation. Share must
// be used before creating another owner.
type Handle struct{ lease *lease }

// View is a borrowed read-only view. It does not retain the backing storage;
// the owner must outlive every use of the view. Call Share when a retained
// Handle is needed.
type View struct{ lease *lease }

// Allocator owns one bounded payload grant. It is intended to be scoped to a
// Job component or worker; allocation and release update only this local
// owner, never a process-wide pool or resource tracker.
type Allocator struct {
	limit int64
	used  atomic.Int64
}

func NewAllocator(limit int64) (*Allocator, error) {
	if limit <= 0 {
		return nil, ErrInvalidLimit
	}
	return &Allocator{limit: limit}, nil
}

func (a *Allocator) Limit() int64 {
	if a == nil {
		return 0
	}
	return a.limit
}

func (a *Allocator) Used() int64 {
	if a == nil {
		return 0
	}
	return a.used.Load()
}

// Allocate charges the logical layout size (at least one byte) to the grant.
// The backing slice reserves up to Alignment-1 additional bytes solely to
// align the exposed payload; that implementation slack is not grant capacity.
func (a *Allocator) Allocate(spec Spec) (Handle, error) {
	layout, rawSize, err := layoutOf(spec)
	if err != nil {
		return Handle{}, err
	}
	charge := int64(rawSize)
	if err := a.reserve(charge); err != nil {
		return Handle{}, err
	}
	state := allocateStorage(layout, rawSize)
	state.charge = charge
	state.repay = a.release
	return Handle{lease: newLease(state, 0, layout)}, nil
}

func (a *Allocator) FromBytes(value []byte, alignment int) (Handle, error) {
	lease, err := a.Overwrite(Spec{Alignment: alignment, Planes: []PlaneSpec{{Size: len(value)}}})
	if err != nil {
		return Handle{}, err
	}
	if err := lease.Fill(func(destination Mutable) error {
		copy(destination.Bytes(), value)
		return nil
	}); err != nil {
		return Handle{}, err
	}
	return lease.Commit()
}

// Overwrite reserves private backing memory without publishing a readable
// Handle. The caller can access it only inside WriteLease.Fill and receives an
// owned Handle only after a successful Commit.
func (a *Allocator) Overwrite(spec Spec) (*WriteLease, error) {
	handle, err := a.Allocate(spec)
	if err != nil {
		return nil, err
	}
	return &WriteLease{handle: handle}, nil
}

func (a *Allocator) reserve(size int64) error {
	if a == nil || size <= 0 {
		return ErrInvalidLimit
	}
	for {
		used := a.used.Load()
		if size > a.limit-used {
			return ErrLimit
		}
		if a.used.CompareAndSwap(used, used+size) {
			return nil
		}
	}
}

func (a *Allocator) release(size int64) {
	if a != nil && size > 0 {
		a.used.Add(-size)
	}
}

// layoutOf resolves a plane specification into absolute offsets. A layout is
// content the caller controls, so every sum is checked before it is made: an
// unchecked int addition can overflow twice and land back on a small positive
// size, which would record a plane far outside a backing allocation that the
// allocator then under-charges.
func layoutOf(spec Spec) (Layout, int, error) {
	alignment := spec.Alignment
	if alignment == 0 {
		alignment = 1
	}
	if alignment < 1 || alignment&(alignment-1) != 0 {
		return Layout{}, 0, ErrInvalidAlignment
	}
	planes := make([]Plane, len(spec.Planes))
	position := 0
	for index, plane := range spec.Planes {
		if plane.Size < 0 || plane.Padding < 0 {
			return Layout{}, 0, fmt.Errorf("plane %d: %w", index, ErrInvalidPlane)
		}
		offset, ok := alignUp(position, alignment)
		if !ok {
			return Layout{}, 0, planeOverflow(index)
		}
		extent, ok := checkedAdd(plane.Size, plane.Padding)
		if !ok {
			return Layout{}, 0, planeOverflow(index)
		}
		end, ok := checkedAdd(offset, extent)
		if !ok {
			return Layout{}, 0, planeOverflow(index)
		}
		planes[index] = Plane{Offset: offset, Size: plane.Size, Padding: plane.Padding}
		position = end
	}
	rawSize := position
	if rawSize == 0 {
		rawSize = 1
	}
	if rawSize > maxInt-(alignment-1) {
		return Layout{}, 0, ErrLayoutOverflow
	}
	layout := Layout{Alignment: alignment, Size: position, Planes: planes, ReadOnly: spec.ReadOnly, Shared: spec.Shared}
	if err := validateLayout(layout); err != nil {
		return Layout{}, 0, err
	}
	return layout, rawSize, nil
}

// validateLayout asserts the invariant every reader relies on: each plane,
// including its padding, lies inside the allocation the layout describes.
func validateLayout(layout Layout) error {
	if layout.Size < 0 {
		return ErrLayoutOverflow
	}
	for index, plane := range layout.Planes {
		if plane.Offset < 0 || plane.Size < 0 || plane.Padding < 0 {
			return fmt.Errorf("plane %d: %w", index, ErrInvalidPlane)
		}
		extent, ok := checkedAdd(plane.Size, plane.Padding)
		if !ok {
			return planeOverflow(index)
		}
		end, ok := checkedAdd(plane.Offset, extent)
		if !ok || end > layout.Size {
			return planeOverflow(index)
		}
	}
	return nil
}

func planeOverflow(index int) error {
	return fmt.Errorf("plane %d: %w", index, ErrLayoutOverflow)
}

func checkedAdd(left, right int) (int, bool) {
	if right > maxInt-left {
		return 0, false
	}
	return left + right, true
}

func alignUp(value, alignment int) (int, bool) {
	mask := alignment - 1
	if value > maxInt-mask {
		return 0, false
	}
	return (value + mask) &^ mask, true
}

func allocateStorage(layout Layout, rawSize int) *storage {
	alignment := layout.Alignment
	raw := make([]byte, rawSize+alignment-1)
	base := uintptr(unsafe.Pointer(&raw[0]))
	offset := int((uintptr(alignment) - base%uintptr(alignment)) % uintptr(alignment))
	data := raw[offset : offset+layout.Size : offset+layout.Size]
	state := &storage{data: data, layout: layout}
	state.refs.Store(1)
	return state
}

func newLease(state *storage, offset int, layout Layout) *lease {
	return &lease{storage: state, offset: offset, layout: layout}
}

func (l *lease) bytes() []byte {
	if l == nil || l.storage == nil {
		return nil
	}
	return l.storage.data[l.offset : l.offset+l.layout.Size : l.offset+l.layout.Size]
}

func (h Handle) Valid() bool {
	return h.lease != nil && h.lease.storage != nil && h.lease.storage.refs.Load() > 0 && !h.lease.released.Load()
}

func (h Handle) Borrow() View {
	if !h.Valid() {
		return View{}
	}
	return View{lease: h.lease}
}

func (h Handle) Share() Handle {
	if !h.Valid() || !h.lease.storage.retain() {
		return Handle{}
	}
	return Handle{lease: newLease(h.lease.storage, h.lease.offset, h.lease.layout)}
}

// Range returns an owned zero-copy view over a byte range of h. The result is
// read-only, has one plane, and retains the same backing storage independently
// of the original Handle.
func (h Handle) Range(offset, size int) (Handle, error) {
	if !h.Valid() {
		return Handle{}, ErrInvalidHandle
	}
	if offset < 0 || size < 0 || offset > h.lease.layout.Size || size > h.lease.layout.Size-offset {
		return Handle{}, ErrRange
	}
	if !h.lease.storage.retain() {
		return Handle{}, ErrInvalidHandle
	}
	layout := Layout{
		Alignment: 1,
		Size:      size,
		Planes:    []Plane{{Size: size}},
		ReadOnly:  true,
		Shared:    true,
	}
	return Handle{lease: newLease(h.lease.storage, h.lease.offset+offset, layout)}, nil
}

func (h Handle) Layout() Layout { return h.Borrow().Layout() }

func (h Handle) Bytes() Bytes { return h.Borrow().Bytes() }

func (h Handle) Plane(index int) (Bytes, error) { return h.Borrow().Plane(index) }

// PlaneAligned reports whether a plane begins at the requested power-of-two
// alignment without exposing its address.
func (h Handle) PlaneAligned(index, alignment int) (bool, error) {
	return h.Borrow().PlaneAligned(index, alignment)
}

func (h Handle) ReadOnly() bool {
	return h.Valid() && h.lease.layout.ReadOnly
}

func (h Handle) Shared() bool {
	return h.Valid() && h.lease.layout.Shared
}

// Edit is a transactional mutable view. An exclusive writable Handle reuses
// its backing storage. A shared or read-only Handle copies into the supplied
// allocator, and consumes the original owner only when Commit succeeds.
type Edit struct {
	original Handle
	working  Handle
	copied   bool
	active   bool
}

func (h Handle) Edit(allocator *Allocator) (Edit, error) {
	if !h.Valid() {
		return Edit{}, ErrInvalidHandle
	}
	storage := h.lease.storage
	if !h.lease.layout.ReadOnly && !h.lease.layout.Shared && storage.refs.Load() == 1 {
		return Edit{original: h, working: h, active: true}, nil
	}
	if allocator == nil {
		return Edit{}, ErrEditAllocator
	}
	working, err := allocator.Allocate(editSpec(h.lease.layout))
	if err != nil {
		return Edit{}, err
	}
	mutable, err := working.mutableBytes()
	if err != nil {
		working.Release()
		return Edit{}, err
	}
	h.Bytes().CopyTo(mutable)
	return Edit{original: h, working: working, copied: true, active: true}, nil
}

func editSpec(layout Layout) Spec {
	planes := make([]PlaneSpec, len(layout.Planes))
	for index, plane := range layout.Planes {
		planes[index] = PlaneSpec{Size: plane.Size, Padding: plane.Padding}
	}
	return Spec{Planes: planes, Alignment: layout.Alignment}
}

// Handle returns the candidate owner to publish downstream. It does not
// retain the storage and remains governed by Commit or Discard.
func (e *Edit) Handle() Handle {
	if e == nil || !e.active {
		return Handle{}
	}
	return e.working
}

func (e *Edit) MutableBytes() ([]byte, error) {
	if e == nil || !e.active || !e.working.Valid() {
		return nil, ErrLeaseState
	}
	return e.working.mutableBytes()
}

func (e *Edit) MutablePlane(index int) ([]byte, error) {
	if e == nil || !e.active || !e.working.Valid() {
		return nil, ErrLeaseState
	}
	planes := e.working.lease.layout.Planes
	if index < 0 || index >= len(planes) {
		return nil, ErrPlaneIndex
	}
	data, err := e.working.mutableBytes()
	if err != nil {
		return nil, err
	}
	return planeSlice(data, planes[index])
}

func (e *Edit) Copied() bool { return e != nil && e.active && e.copied }

// Commit transfers the candidate Handle previously returned by Handle to its
// consumer. For copy-on-write it consumes this branch's original owner.
func (e *Edit) Commit() error {
	if e == nil || !e.active {
		return ErrLeaseState
	}
	if e.copied {
		e.original.Release()
	}
	e.original = Handle{}
	e.working = Handle{}
	e.active = false
	return nil
}

// Discard abandons an edit after a downstream failure. A copied candidate is
// released while the original owner remains untouched; an exclusive edit
// leaves that owner for the runtime to release.
func (e *Edit) Discard() {
	if e == nil || !e.active {
		return
	}
	if e.copied {
		e.working.Release()
	}
	e.original = Handle{}
	e.working = Handle{}
	e.active = false
}

func (h Handle) Release() {
	if h.lease == nil || !h.lease.released.CompareAndSwap(false, true) {
		return
	}
	if h.lease.storage.refs.Add(-1) == 0 {
		state := h.lease.storage
		state.data = nil
		if state.repay != nil {
			state.repay(state.charge)
			state.repay = nil
		}
	}
}

func (v View) Valid() bool {
	return v.lease != nil && v.lease.storage != nil && !v.lease.released.Load() && v.lease.storage.refs.Load() > 0
}

func (v View) Share() Handle {
	if !v.Valid() || !v.lease.storage.retain() {
		return Handle{}
	}
	return Handle{lease: newLease(v.lease.storage, v.lease.offset, v.lease.layout)}
}

// Layout returns an independent copy, so a caller cannot reach the planes this
// view describes by writing into the slice it was handed. Reading only how
// many there are does not need that copy; use PlaneCount.
func (v View) Layout() Layout {
	if !v.Valid() {
		return Layout{}
	}
	return v.lease.layout.Clone()
}

// PlaneCount reports how many planes this view describes without copying what
// describes them. Every stage checks the count against the channels it was
// compiled for, once per frame, and a check that allocated would put that cost
// on every stage of every chain.
func (v View) PlaneCount() int {
	if !v.Valid() {
		return 0
	}
	return len(v.lease.layout.Planes)
}

func (v View) Bytes() Bytes {
	if !v.Valid() {
		return Bytes{}
	}
	return Bytes{lease: v.lease, size: v.lease.layout.Size}
}

func (v View) Plane(index int) (Bytes, error) {
	if !v.Valid() {
		return Bytes{}, errors.New("invalid buffer view")
	}
	planes := v.lease.layout.Planes
	if index < 0 || index >= len(planes) {
		return Bytes{}, ErrPlaneIndex
	}
	plane := planes[index]
	return Bytes{lease: v.lease, offset: plane.Offset, size: plane.Size}, nil
}

func (v View) PlaneAligned(index, alignment int) (bool, error) {
	if !v.Valid() {
		return false, ErrInvalidHandle
	}
	if alignment < 1 || alignment&(alignment-1) != 0 {
		return false, ErrInvalidAlignment
	}
	planes := v.lease.layout.Planes
	if index < 0 || index >= len(planes) {
		return false, ErrPlaneIndex
	}
	if planes[index].Size == 0 {
		return true, nil
	}
	plane, err := planeSlice(v.lease.bytes(), planes[index])
	if err != nil {
		return false, err
	}
	return uintptr(unsafe.Pointer(&plane[0]))%uintptr(alignment) == 0, nil
}

func (h Handle) mutableBytes() ([]byte, error) {
	if !h.Valid() {
		return nil, ErrInvalidHandle
	}
	if h.lease.layout.ReadOnly {
		return nil, ErrReadOnly
	}
	if h.lease.layout.Shared || h.lease.storage.refs.Load() != 1 {
		return nil, ErrShared
	}
	return h.lease.bytes(), nil
}

func (v View) ReadOnly() bool {
	return v.Valid() && v.lease.layout.ReadOnly
}

func (v View) Shared() bool {
	return v.Valid() && v.lease.layout.Shared
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

// Mutable is a write view whose lifetime is restricted to a WriteLease.Fill
// callback. Retaining one of its slices beyond the callback violates the
// overwrite contract and is checked by conformance tests.
type Mutable struct{ storage *storage }

func (m Mutable) Bytes() []byte {
	if m.storage == nil {
		return nil
	}
	return m.storage.data
}

func (m Mutable) Plane(index int) ([]byte, error) {
	if m.storage == nil {
		return nil, ErrLeaseState
	}
	planes := m.storage.layout.Planes
	if index < 0 || index >= len(planes) {
		return nil, ErrPlaneIndex
	}
	return planeSlice(m.storage.data, planes[index])
}

// planeSlice refuses a plane that does not fit the allocation it was recorded
// against. layoutOf already rejects such a plane, so reaching this is a bug in
// the buffer package rather than in a caller; returning an error keeps it from
// becoming a slice-bounds panic inside a third-party component.
func planeSlice(data []byte, plane Plane) ([]byte, error) {
	end, ok := checkedAdd(plane.Offset, plane.Size)
	if !ok || plane.Offset < 0 || plane.Size < 0 || end > len(data) {
		return nil, ErrRange
	}
	return data[plane.Offset:end:end], nil
}

type writeLeaseState uint32

const (
	leaseWritable writeLeaseState = iota
	leaseFilling
	leaseFilled
	leaseCommitted
	leaseDiscarded
)

// WriteLease prevents a partially initialized allocation from becoming a
// readable payload. Fill may be called once; Commit is available only after a
// successful Fill. Discard is idempotent.
type WriteLease struct {
	handle Handle
	state  atomic.Uint32
}

func (l *WriteLease) Fill(write func(Mutable) error) error {
	if l == nil || write == nil || !l.state.CompareAndSwap(uint32(leaseWritable), uint32(leaseFilling)) {
		return ErrLeaseState
	}
	if err := write(Mutable{storage: l.handle.lease.storage}); err != nil {
		l.Discard()
		return err
	}
	if !l.state.CompareAndSwap(uint32(leaseFilling), uint32(leaseFilled)) {
		return ErrLeaseState
	}
	return nil
}

func (l *WriteLease) Commit() (Handle, error) {
	if l == nil || !l.state.CompareAndSwap(uint32(leaseFilled), uint32(leaseCommitted)) {
		return Handle{}, ErrLeaseState
	}
	handle := l.handle
	l.handle = Handle{}
	return handle, nil
}

func (l *WriteLease) Discard() {
	if l == nil {
		return
	}
	for {
		state := writeLeaseState(l.state.Load())
		if state == leaseCommitted || state == leaseDiscarded {
			return
		}
		if l.state.CompareAndSwap(uint32(state), uint32(leaseDiscarded)) {
			l.handle.Release()
			l.handle = Handle{}
			return
		}
	}
}
