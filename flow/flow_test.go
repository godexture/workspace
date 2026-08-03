package flow

import (
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/media/schema"
)

type flowUnitID struct{}
type flowUnit struct{ Value int }

func flowSchema() schema.Type[flowUnit] {
	return schema.Define[flowUnitID, flowUnit](schema.Traits[flowUnit]{})
}

func TestShapeValidatesTypedPorts(t *testing.T) {
	typ := flowSchema()
	shape := NewShape([]Port{In("input", typ)}, []Port{Out("output", typ, Optional())})
	if err := shape.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := NewShape([]Port{In("same", typ)}, []Port{Out("same", typ)}).Validate(); err == nil {
		t.Fatal("duplicate port id accepted")
	}
	if err := (Shape{}).Validate(); err == nil {
		t.Fatal("empty shape accepted")
	}
}

func TestInputOwnershipMoveFanoutFailureAndDrop(t *testing.T) {
	var drops atomic.Int32
	input := NewInput(42, func(int) { drops.Add(1) })
	shared := input.Share()
	owned := input.Take()
	if input.Valid() || !owned.Valid() || !shared.Valid() {
		t.Fatal("ownership state after share/take is invalid")
	}
	owned.Release()
	if drops.Load() != 0 {
		t.Fatal("owner release dropped while retained share exists")
	}
	shared.Release()
	if drops.Load() != 1 {
		t.Fatalf("drop count after fan-out = %d", drops.Load())
	}

	failed := NewInput(7, func(int) { drops.Add(1) })
	// A failed writer leaves the input untouched; the caller can drop it.
	failed.Drop()
	if drops.Load() != 2 {
		t.Fatalf("drop count after failed write = %d", drops.Load())
	}

	second := NewInput(8, func(int) { drops.Add(1) })
	second.Drop()
	second.Drop()
	if drops.Load() != 3 {
		t.Fatalf("idempotent drop count = %d", drops.Load())
	}
}
