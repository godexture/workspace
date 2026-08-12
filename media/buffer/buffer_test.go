package buffer

import (
	"bytes"
	"errors"
	"testing"
)

func testAllocator(t *testing.T, limit int64) *Allocator {
	t.Helper()
	allocator, err := NewAllocator(limit)
	if err != nil {
		t.Fatal(err)
	}
	return allocator
}

func TestAllocateUsesOneAlignedBackingAndPlaneLayout(t *testing.T) {
	allocator := testAllocator(t, 1024)
	handle, err := allocator.Allocate(Spec{
		Alignment: 16,
		Planes:    []PlaneSpec{{Size: 5, Padding: 3}, {Size: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	layout := handle.Layout()
	if layout.Size != len(handle.Bytes()) || len(layout.Planes) != 2 {
		t.Fatalf("layout = %#v, bytes = %d", layout, len(handle.Bytes()))
	}
	for _, plane := range layout.Planes {
		if plane.Offset%layout.Alignment != 0 {
			t.Fatalf("plane offset %d is not aligned to %d", plane.Offset, layout.Alignment)
		}
	}
	first, _ := handle.Plane(0)
	second, _ := handle.Plane(1)
	first[0] = 4
	if handle.Bytes()[layout.Planes[0].Offset] != 4 || len(second) != 7 {
		t.Fatal("plane is not a view over the backing buffer")
	}
	if allocator.Used() == 0 {
		t.Fatal("allocator did not account for the live payload")
	}
}

func TestHandleSharingAndReadOnlyBoundary(t *testing.T) {
	allocator := testAllocator(t, 64)
	handle, err := allocator.Allocate(Spec{Alignment: 8, ReadOnly: true, Shared: true, Planes: []PlaneSpec{{Size: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	shared := handle.Share()
	handle.Release()
	if !shared.Valid() || !shared.ReadOnly() || !shared.Shared() {
		t.Fatal("shared handle did not retain backing storage")
	}
	if _, err := shared.MutableBytes(); err != ErrReadOnly {
		t.Fatalf("mutable view error = %v", err)
	}
	shared.Release()
	if shared.Valid() {
		t.Fatal("released handle remains valid")
	}
	if allocator.Used() != 0 {
		t.Fatalf("released allocation still charges %d bytes", allocator.Used())
	}
	writable, err := allocator.Allocate(Spec{Planes: []PlaneSpec{{Size: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	retained := writable.Share()
	if _, err := writable.MutableBytes(); !errors.Is(err, ErrShared) {
		t.Fatalf("shared mutable view error = %v", err)
	}
	writable.Release()
	retained.Release()
}

func TestRangeRetainsReadOnlySinglePlaneView(t *testing.T) {
	allocator := testAllocator(t, 64)
	original, err := allocator.FromBytes([]byte{0, 1, 2, 3, 4, 5, 6, 7}, 8)
	if err != nil {
		t.Fatal(err)
	}
	ranged, err := original.Range(2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !original.Valid() || !bytes.Equal(original.Bytes(), []byte{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("Range consumed or changed the original Handle")
	}
	layout := ranged.Layout()
	if layout.Size != 4 || len(layout.Planes) != 1 || layout.Planes[0] != (Plane{Size: 4}) || !layout.ReadOnly || !layout.Shared {
		t.Fatalf("range layout = %#v", layout)
	}
	if !bytes.Equal(ranged.Bytes(), []byte{2, 3, 4, 5}) {
		t.Fatalf("range bytes = %v", ranged.Bytes())
	}
	if &ranged.Bytes()[0] != &original.Bytes()[2] {
		t.Fatal("Range copied instead of sharing the original storage")
	}
	plane, err := ranged.Plane(0)
	if err != nil || !bytes.Equal(plane, ranged.Bytes()) {
		t.Fatalf("range plane = %v, %v", plane, err)
	}
	if _, err := ranged.MutableBytes(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("range mutable error = %v", err)
	}
	view := ranged.Borrow()
	retained := view.Share()
	ranged.Release()
	if view.Valid() {
		t.Fatal("borrowed range outlived its owner")
	}
	original.Release()
	if !retained.Valid() || !bytes.Equal(retained.Bytes(), []byte{2, 3, 4, 5}) || allocator.Used() == 0 {
		t.Fatal("retained range did not keep the original storage and overlay alive")
	}
	retained.Release()
	if allocator.Used() != 0 {
		t.Fatalf("released range retained %d bytes", allocator.Used())
	}
}

func TestRangeRejectsInvalidBounds(t *testing.T) {
	allocator := testAllocator(t, 16)
	handle, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	for _, bounds := range [][2]int{{-1, 1}, {0, -1}, {5, 0}, {3, 2}} {
		if _, err := handle.Range(bounds[0], bounds[1]); !errors.Is(err, ErrRange) {
			t.Fatalf("Range(%d, %d) error = %v", bounds[0], bounds[1], err)
		}
	}
	empty, err := handle.Range(4, 0)
	if err != nil || !empty.Valid() || len(empty.Bytes()) != 0 {
		t.Fatalf("empty tail range = valid %v, bytes %v, error %v", empty.Valid(), empty.Bytes(), err)
	}
	empty.Release()
	if _, err := (Handle{}).Range(0, 0); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("invalid Handle range error = %v", err)
	}
}

func TestRangeEditCopiesOnlyVisibleBytes(t *testing.T) {
	allocator := testAllocator(t, 64)
	original, err := allocator.FromBytes([]byte{0, 1, 2, 3, 4, 5}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ranged, err := original.Range(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := ranged.Edit(allocator)
	if err != nil {
		t.Fatal(err)
	}
	if !edit.Copied() || edit.Handle().Layout().Size != 3 {
		t.Fatal("range edit did not copy only the visible layout")
	}
	mutable, err := edit.MutableBytes()
	if err != nil {
		t.Fatal(err)
	}
	mutable[0] = 9
	candidate := edit.Handle()
	if err := edit.Commit(); err != nil {
		t.Fatal(err)
	}
	if ranged.Valid() || !bytes.Equal(candidate.Bytes(), []byte{9, 3, 4}) || !bytes.Equal(original.Bytes(), []byte{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("range edit = ranged %v, candidate %v, original %v", ranged.Valid(), candidate.Bytes(), original.Bytes())
	}
	candidate.Release()
	original.Release()
	if allocator.Used() != 0 {
		t.Fatalf("range edit retained %d bytes", allocator.Used())
	}
}

func TestEditCopiesSharedStorageTransactionally(t *testing.T) {
	allocator := testAllocator(t, 64)
	original, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 4)
	if err != nil {
		t.Fatal(err)
	}
	shared := original.Share()
	edit, err := original.Edit(allocator)
	if err != nil {
		t.Fatal(err)
	}
	if !edit.Copied() {
		t.Fatal("shared edit reused the original backing")
	}
	candidate := edit.Handle()
	bytes, err := edit.MutableBytes()
	if err != nil {
		t.Fatal(err)
	}
	bytes[0] = 9
	if shared.Bytes()[0] != 1 {
		t.Fatal("copy-on-write changed the shared original")
	}
	if err := edit.Commit(); err != nil {
		t.Fatal(err)
	}
	if original.Valid() || !candidate.Valid() || candidate.Bytes()[0] != 9 {
		t.Fatalf("committed edit = original %v candidate %v bytes %v", original.Valid(), candidate.Valid(), candidate.Bytes())
	}
	shared.Release()
	candidate.Release()
	if allocator.Used() != 0 {
		t.Fatalf("committed edit retained %d bytes", allocator.Used())
	}
}

func TestEditDiscardKeepsOriginalOwner(t *testing.T) {
	allocator := testAllocator(t, 64)
	original, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 4)
	if err != nil {
		t.Fatal(err)
	}
	shared := original.Share()
	edit, err := original.Edit(allocator)
	if err != nil {
		t.Fatal(err)
	}
	candidate := edit.Handle()
	bytes, _ := edit.MutableBytes()
	bytes[0] = 9
	edit.Discard()
	if !original.Valid() || !shared.Valid() || candidate.Valid() || original.Bytes()[0] != 1 {
		t.Fatalf("discarded edit = original %v shared %v candidate %v bytes %v", original.Valid(), shared.Valid(), candidate.Valid(), original.Bytes())
	}
	original.Release()
	shared.Release()
	if allocator.Used() != 0 {
		t.Fatalf("discarded edit retained %d bytes", allocator.Used())
	}
}

func TestFromBytesCopiesInput(t *testing.T) {
	input := []byte{1, 2, 3}
	allocator := testAllocator(t, 64)
	handle, err := allocator.FromBytes(input, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	input[0] = 9
	if handle.Bytes()[0] != 1 {
		t.Fatal("buffer retained the caller's mutable slice")
	}
}

func TestAllocatorEnforcesLocalGrantAndReturnsItOnce(t *testing.T) {
	allocator := testAllocator(t, 4)
	first, err := allocator.Allocate(Spec{Planes: []PlaneSpec{{Size: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Allocate(Spec{Planes: []PlaneSpec{{Size: 1}}}); !errors.Is(err, ErrLimit) {
		t.Fatalf("grant exhaustion error = %v", err)
	}
	shared := first.Share()
	first.Release()
	if allocator.Used() != 4 {
		t.Fatalf("shared allocation charge = %d", allocator.Used())
	}
	first.Release()
	shared.Release()
	if allocator.Used() != 0 {
		t.Fatalf("returned grant = %d", allocator.Used())
	}
	second, err := allocator.Allocate(Spec{Planes: []PlaneSpec{{Size: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
}

func TestAllocatorGrantExcludesAlignmentBackingSlack(t *testing.T) {
	spec := Spec{Alignment: 8, Planes: []PlaneSpec{{Size: 4}}}
	allocator := testAllocator(t, 4)
	handle, err := allocator.Allocate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if allocator.Used() != 4 {
		t.Fatalf("logical payload charge = %d", allocator.Used())
	}
	handle.Release()
	if allocator.Used() != 0 {
		t.Fatalf("logical payload repayment = %d", allocator.Used())
	}
}

func TestOverwritePublishesOnlyAfterSuccessfulFill(t *testing.T) {
	allocator := testAllocator(t, 16)
	lease, err := allocator.Overwrite(Spec{Planes: []PlaneSpec{{Size: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Commit(); !errors.Is(err, ErrLeaseState) {
		t.Fatalf("early commit error = %v", err)
	}
	if err := lease.Fill(func(value Mutable) error {
		copy(value.Bytes(), []byte{1, 2, 3, 4})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handle, err := lease.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if got := handle.Bytes(); len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Fatalf("committed bytes = %v", got)
	}
	handle.Release()
	if allocator.Used() != 0 {
		t.Fatalf("committed handle retained %d bytes after release", allocator.Used())
	}
}

func TestOverwriteDiscardsFillFailureAndExplicitCancel(t *testing.T) {
	allocator := testAllocator(t, 8)
	want := errors.New("write failed")
	lease, err := allocator.Overwrite(Spec{Planes: []PlaneSpec{{Size: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Fill(func(Mutable) error { return want }); !errors.Is(err, want) {
		t.Fatalf("fill error = %v", err)
	}
	if allocator.Used() != 0 {
		t.Fatalf("failed fill retained %d bytes", allocator.Used())
	}
	lease, err = allocator.Overwrite(Spec{Planes: []PlaneSpec{{Size: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	lease.Discard()
	lease.Discard()
	if allocator.Used() != 0 {
		t.Fatalf("discard retained %d bytes", allocator.Used())
	}
}
