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
	result, err := a.Allocate(Spec{Alignment: alignment, Planes: []PlaneSpec{{Size: len(value)}}})
	if err != nil {
		return Handle{}, err
	}
	mutable, err := result.MutableBytes()
	if err != nil {
		result.Release()
		return Handle{}, err
	}
	copy(mutable, value)
	return result, nil
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
		position = align(position, alignment)
		planes[index] = Plane{Offset: position, Size: plane.Size, Padding: plane.Padding}
		position += plane.Size + plane.Padding
		if position < 0 {
			return Layout{}, 0, errors.New("buffer size overflow")
		}
	}
	rawSize := position
	if rawSize == 0 {
		rawSize = 1
	}
	if rawSize > int(^uint(0)>>1)-(alignment-1) {
		return Layout{}, 0, errors.New("buffer size overflow")
	}
	return Layout{Alignment: alignment, Size: position, Planes: planes, ReadOnly: spec.ReadOnly, Shared: spec.Shared}, rawSize, nil
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

func align(value, alignment int) int {
	mask := alignment - 1
	return (value + mask) &^ mask
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

func (h Handle) Bytes() []byte { return h.Borrow().Bytes() }

func (h Handle) MutableBytes() ([]byte, error) {
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

func (h Handle) Plane(index int) ([]byte, error) { return h.Borrow().Plane(index) }

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
	copy(working.Bytes(), h.Bytes())
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
	return e.working.MutableBytes()
}

func (e *Edit) Plane(index int) ([]byte, error) {
	if e == nil || !e.active || !e.working.Valid() {
		return nil, ErrLeaseState
	}
	return e.working.Plane(index)
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

func (v View) Layout() Layout {
	if !v.Valid() {
		return Layout{}
	}
	return v.lease.layout.Clone()
}

func (v View) Bytes() []byte {
	if !v.Valid() {
		return nil
	}
	return v.lease.bytes()
}

func (v View) Plane(index int) ([]byte, error) {
	if !v.Valid() {
		return nil, errors.New("invalid buffer view")
	}
	planes := v.lease.layout.Planes
	if index < 0 || index >= len(planes) {
		return nil, ErrPlaneIndex
	}
	plane := planes[index]
	bytes := v.lease.bytes()
	return bytes[plane.Offset : plane.Offset+plane.Size : plane.Offset+plane.Size], nil
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
	plane := planes[index]
	return m.storage.data[plane.Offset : plane.Offset+plane.Size : plane.Offset+plane.Size], nil
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
