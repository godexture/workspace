package access

import (
	"errors"
	"testing"

	"github.com/godexture/godec/media/buffer"
)

func TestBytesSchemaRetainsAndReleasesGrantBackedPayload(t *testing.T) {
	allocator, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatal(err)
	}
	traits := Bytes().Traits()
	if traits.Fork == nil || traits.Drop == nil || traits.Size == nil || traits.Time != nil || traits.Size(handle) != 4 {
		t.Fatalf("byte schema traits = %#v", traits)
	}
	shared := traits.Fork(handle)
	traits.Drop(handle)
	if !shared.Valid() || allocator.Used() == 0 {
		t.Fatal("byte schema did not retain shared grant storage")
	}
	traits.Drop(shared)
	if allocator.Used() != 0 {
		t.Fatalf("byte schema retained %d bytes", allocator.Used())
	}
}

func TestCarrierTimeBaseIsAValidCanonicalPlaceholder(t *testing.T) {
	base := CarrierTimeBase()
	if !base.Valid() || base.Numerator != 1 || base.Denominator != 1 {
		t.Fatalf("carrier time base = %#v", base)
	}
	if Bytes().Traits().Time != nil {
		t.Fatal("byte carriers must not expose a media timeline")
	}
}

func TestWritesSchemaRetainsAndReleasesPayload(t *testing.T) {
	allocator, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes([]byte{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatal(err)
	}
	write, err := Patch(7, handle)
	if err != nil {
		t.Fatal(err)
	}
	if !write.Valid() || write.Operation() != PatchOperation || write.Offset() != 7 {
		t.Fatalf("write = %#v", write)
	}
	traits := Writes().Traits()
	if traits.Fork == nil || traits.Drop == nil || traits.Size == nil || traits.Time != nil || traits.Size(write) != 4 {
		t.Fatalf("write schema traits = %#v", traits)
	}
	shared := traits.Fork(write)
	traits.Drop(write)
	if !shared.Valid() || allocator.Used() == 0 {
		t.Fatal("write schema did not retain shared grant storage")
	}
	traits.Drop(shared)
	if allocator.Used() != 0 {
		t.Fatalf("write schema retained %d bytes", allocator.Used())
	}
}

func TestWriteConstructorsRejectInvalidPayloadAndOffset(t *testing.T) {
	if _, err := Append(buffer.Handle{}); !errors.Is(err, ErrInvalidWrite) {
		t.Fatalf("invalid append error = %v", err)
	}
	allocator, err := buffer.NewAllocator(1)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes([]byte{1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Patch(-1, handle); !errors.Is(err, ErrInvalidWrite) {
		handle.Release()
		t.Fatalf("negative patch error = %v", err)
	}
	if !handle.Valid() {
		t.Fatal("failed construction consumed its payload")
	}
	handle.Release()
}
