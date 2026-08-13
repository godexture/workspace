package buffer

import (
	"errors"
	"io"
	"slices"
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
	if layout.Size != handle.Bytes().Len() || len(layout.Planes) != 2 {
		t.Fatalf("layout = %#v, bytes = %d", layout, handle.Bytes().Len())
	}
	for _, plane := range layout.Planes {
		if plane.Offset%layout.Alignment != 0 {
			t.Fatalf("plane offset %d is not aligned to %d", plane.Offset, layout.Alignment)
		}
	}
	edit, err := handle.Edit(nil)
	if err != nil {
		t.Fatal(err)
	}
	mutable, err := edit.MutablePlane(0)
	if err != nil {
		t.Fatal(err)
	}
	mutable[0] = 4
	handle = edit.Handle()
	if err := edit.Commit(); err != nil {
		t.Fatal(err)
	}
	first, _ := handle.Plane(0)
	second, _ := handle.Plane(1)
	if first.Len() != 5 || first.At(0) != 4 || handle.Bytes().At(layout.Planes[0].Offset) != 4 || second.Len() != 7 {
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
	if _, err := writable.Edit(nil); !errors.Is(err, ErrEditAllocator) {
		t.Fatalf("shared edit error = %v", err)
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
	if !original.Valid() || !original.Bytes().EqualSlice([]byte{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("Range consumed or changed the original Handle")
	}
	layout := ranged.Layout()
	if layout.Size != 4 || len(layout.Planes) != 1 || layout.Planes[0] != (Plane{Size: 4}) || !layout.ReadOnly || !layout.Shared {
		t.Fatalf("range layout = %#v", layout)
	}
	if !ranged.Bytes().EqualSlice([]byte{2, 3, 4, 5}) {
		t.Fatalf("range bytes = %v", ranged.Bytes().AppendTo(nil))
	}
	plane, err := ranged.Plane(0)
	if err != nil || !plane.Equal(ranged.Bytes()) {
		t.Fatalf("range plane = %v, %v", plane, err)
	}
	view := ranged.Borrow()
	retained := view.Share()
	ranged.Release()
	if view.Valid() {
		t.Fatal("borrowed range outlived its owner")
	}
	original.Release()
	if !retained.Valid() || !retained.Bytes().EqualSlice([]byte{2, 3, 4, 5}) || allocator.Used() == 0 {
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
	if err != nil || !empty.Valid() || empty.Bytes().Len() != 0 {
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
	if ranged.Valid() || !candidate.Bytes().EqualSlice([]byte{9, 3, 4}) || !original.Bytes().EqualSlice([]byte{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("range edit = ranged %v, candidate %v, original %v", ranged.Valid(), candidate.Bytes().AppendTo(nil), original.Bytes().AppendTo(nil))
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
	if shared.Bytes().At(0) != 1 {
		t.Fatal("copy-on-write changed the shared original")
	}
	if err := edit.Commit(); err != nil {
		t.Fatal(err)
	}
	if original.Valid() || !candidate.Valid() || candidate.Bytes().At(0) != 9 {
		t.Fatalf("committed edit = original %v candidate %v bytes %v", original.Valid(), candidate.Valid(), candidate.Bytes().AppendTo(nil))
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
	if !original.Valid() || !shared.Valid() || candidate.Valid() || original.Bytes().At(0) != 1 {
		t.Fatalf("discarded edit = original %v shared %v candidate %v bytes %v", original.Valid(), shared.Valid(), candidate.Valid(), original.Bytes().AppendTo(nil))
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
	if handle.Bytes().At(0) != 1 {
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
	if got := handle.Bytes(); got.Len() != 4 || got.At(0) != 1 || got.At(3) != 4 {
		t.Fatalf("committed bytes = %v", got.AppendTo(nil))
	}
	handle.Release()
	if allocator.Used() != 0 {
		t.Fatalf("committed handle retained %d bytes after release", allocator.Used())
	}
}

func TestBytesProvidesImmutableViewsAndCopies(t *testing.T) {
	allocator := testAllocator(t, 16)
	handle, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()

	value := handle.Bytes()
	if value.Len() != 4 || value.At(0) != 1 || !value.EqualSlice([]byte{1, 2, 3, 4}) {
		t.Fatalf("bytes = %v", value.AppendTo(nil))
	}
	subview, err := value.Slice(1, 2)
	if err != nil || !subview.EqualSlice([]byte{2, 3}) || !subview.Equal(subview) {
		t.Fatalf("subview = %v, %v", subview.AppendTo(nil), err)
	}
	for _, bounds := range [][2]int{{-1, 1}, {0, -1}, {5, 0}, {3, 2}} {
		if _, err := value.Slice(bounds[0], bounds[1]); !errors.Is(err, ErrRange) {
			t.Fatalf("Slice(%d, %d) error = %v", bounds[0], bounds[1], err)
		}
	}
	destination := []byte{9, 9, 9}
	if copied := value.CopyTo(destination); copied != 3 || destination[0] != 1 || destination[2] != 3 || !value.EqualSlice([]byte{1, 2, 3, 4}) {
		t.Fatalf("copied = %d, source = %v", copied, value.AppendTo(nil))
	}
	destination[0] = 9
	if value.At(0) != 1 {
		t.Fatal("mutable copy changed immutable source")
	}
	appended := value.AppendTo([]byte{0})
	if len(appended) != 5 || appended[0] != 0 || appended[4] != 4 {
		t.Fatalf("appended = %v", appended)
	}
	reader := value.Reader()
	if _, ok := reader.(io.WriterTo); ok {
		t.Fatal("reader exposes backing through io.WriterTo")
	}
	if _, ok := reader.(interface{ Bytes() []byte }); ok {
		t.Fatal("reader exposes backing bytes")
	}
	read, err := io.ReadAll(reader)
	if err != nil || !value.EqualSlice(read) {
		t.Fatalf("reader = %v, %v", read, err)
	}
}

func TestBytesBlocksDrainThroughCallerScratch(t *testing.T) {
	allocator := testAllocator(t, 32)
	payload := []byte{1, 2, 3, 4, 5, 6, 7}
	handle, err := allocator.FromBytes(payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	value := handle.Bytes()

	var drained []byte
	var offsets []int
	scratch := make([]byte, 3)
	if err := value.Blocks(scratch, func(block []byte, offset int) error {
		drained, offsets = append(drained, block...), append(offsets, offset)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(drained, payload) || !slices.Equal(offsets, []int{0, 3, 6}) {
		t.Fatalf("drained = %v at %v", drained, offsets)
	}

	sentinel := errors.New("visit failed")
	visits := 0
	if err := value.Blocks(scratch, func([]byte, int) error { visits++; return sentinel }); !errors.Is(err, sentinel) || visits != 1 {
		t.Fatalf("visit error = %v after %d visits", err, visits)
	}
	if err := value.Blocks(nil, func([]byte, int) error { return nil }); !errors.Is(err, ErrRange) {
		t.Fatalf("empty scratch error = %v", err)
	}

	suffix, err := value.From(5)
	if err != nil || !suffix.EqualSlice([]byte{6, 7}) {
		t.Fatalf("From(5) = %v, %v", suffix.AppendTo(nil), err)
	}
	if _, err := value.From(8); !errors.Is(err, ErrRange) {
		t.Fatalf("From past the end error = %v", err)
	}

	handle.Release()
	if err := value.Blocks(scratch, func([]byte, int) error { return nil }); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("expired Blocks error = %v", err)
	}
	if _, err := value.From(0); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("expired From error = %v", err)
	}
}

func TestBytesExpireWithOriginatingLease(t *testing.T) {
	allocator := testAllocator(t, 16)
	handle, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatal(err)
	}
	view := handle.Borrow()
	value := view.Bytes()
	subview, err := value.Slice(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	reader := value.Reader()
	shared := view.Share()
	sharedValue := shared.Bytes()

	handle.Release()
	if value.Valid() || subview.Valid() {
		t.Fatal("borrowed bytes survived their originating lease")
	}
	if value.Len() != 4 || subview.Len() != 2 {
		t.Fatalf("expired Len = %d and %d; Len reports the recorded range without revalidating", value.Len(), subview.Len())
	}
	destination := []byte{9, 9}
	if value.CopyTo(destination) != 0 || destination[0] != 9 || len(value.AppendTo([]byte{8})) != 1 || value.Equal(sharedValue) || value.EqualSlice([]byte{}) {
		t.Fatal("expired bytes remained readable")
	}
	if _, err := value.Slice(0, 0); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("expired Slice error = %v", err)
	}
	if _, err := reader.Read(destination); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("expired Reader error = %v", err)
	}
	assertPanics(t, func() { value.At(0) })

	if !sharedValue.Valid() || !sharedValue.EqualSlice([]byte{1, 2, 3, 4}) {
		t.Fatal("separately shared lease expired with its sibling")
	}
	shared.Release()
	if sharedValue.Valid() || allocator.Used() != 0 {
		t.Fatalf("released shared bytes = valid %v, retained %d", sharedValue.Valid(), allocator.Used())
	}
}

func assertPanics(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()
	call()
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
