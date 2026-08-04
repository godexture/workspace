package flow

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/media/schema"
)

type flowUnitID struct{}
type flowUnit struct{ Value int }
type flowValueID struct{}

var linearValueSchema = schema.Define[flowValueID, int](schema.Traits[int]{})

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
	if err := NewShape([]Port{In("required-many", typ, Many())}, nil).Validate(); err != nil {
		t.Fatalf("required many port rejected: %v", err)
	}
	optional := In("optional", typ, Optional())
	if optional.Required() || optional.Multiplicity() != One {
		t.Fatalf("optional port = required %v, multiplicity %v", optional.Required(), optional.Multiplicity())
	}
}

func TestShapeReportsInvalidSchemaMarker(t *testing.T) {
	invalid := schema.Define[struct{}](schema.Traits[int]{})
	err := NewShape([]Port{In("invalid", invalid)}, nil).Validate()
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("invalid schema error = %v", err)
	}
}

func TestInputOwnershipMoveFanoutFailureAndDrop(t *testing.T) {
	var drops atomic.Int32
	typ := schema.Define[flowValueID, int](schema.Traits[int]{
		Fork: func(value int) int { return value },
		Drop: func(int) { drops.Add(1) },
	})
	input := NewInput(42, typ)
	shared := input.Share()
	owned := input.Take()
	if !input.Valid() || !owned.Valid() || !shared.Valid() {
		t.Fatal("value input state after share/take is invalid")
	}
	owned.Release()
	shared.Release()
	if drops.Load() != 2 {
		t.Fatalf("drop count after fan-out = %d", drops.Load())
	}

	failed := NewInput(7, typ)
	// A failed writer leaves the input untouched; the caller can drop it.
	failed.Drop()
	if drops.Load() != 3 {
		t.Fatalf("drop count after failed write = %d", drops.Load())
	}
}

func TestInputCanUseThirdPartyTraitsWithoutFlowState(t *testing.T) {
	var drops atomic.Int32
	input := NewInputWithTraits(8, func(value int) int { return value + 1 }, func(int) { drops.Add(1) })
	shared := input.Share()
	if shared.Value() != 9 {
		t.Fatal("third-party fork trait was not used")
	}
	shared.Release()
	input.Drop()
	if drops.Load() != 2 {
		t.Fatalf("drop count = %d", drops.Load())
	}
}

func TestLinearInputTakeHasNoAllocation(t *testing.T) {
	allocations := testing.AllocsPerRun(1000, func() {
		input := NewInput(1, linearValueSchema)
		owner := input.Take()
		if !owner.Valid() {
			t.Fatal("linear input was invalid")
		}
		owner.Release()
	})
	if allocations != 0 {
		t.Fatalf("linear input hop allocations = %v, want 0", allocations)
	}
}
