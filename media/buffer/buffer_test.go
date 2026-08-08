package buffer

import (
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
