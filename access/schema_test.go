package access

import (
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
