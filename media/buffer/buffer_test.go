package buffer

import "testing"

func TestAllocateUsesOneAlignedBackingAndPlaneLayout(t *testing.T) {
	handle, err := Allocate(Spec{
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
}

func TestHandleSharingAndReadOnlyBoundary(t *testing.T) {
	handle, err := Allocate(Spec{Alignment: 8, ReadOnly: true, Shared: true, Planes: []PlaneSpec{{Size: 4}}})
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
}

func TestFromBytesCopiesInput(t *testing.T) {
	input := []byte{1, 2, 3}
	handle, err := FromBytes(input, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	input[0] = 9
	if handle.Bytes()[0] != 1 {
		t.Fatal("buffer retained the caller's mutable slice")
	}
}
