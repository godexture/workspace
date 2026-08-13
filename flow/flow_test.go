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
	if err := NewShape([]Port{In("required-many", typ, Many(), WithFanIn(ZipFanIn))}, nil).Validate(); err != nil {
		t.Fatalf("required many port rejected: %v", err)
	}
	if err := NewShape([]Port{In("missing-policy", typ, Many())}, nil).Validate(); err == nil {
		t.Fatal("many input without fan-in policy accepted")
	}
	if err := NewShape([]Port{In("invalid-policy", typ, WithFanIn(ZipFanIn))}, nil).Validate(); err == nil {
		t.Fatal("one input with fan-in policy accepted")
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

func TestShapeEqualityUsesSchemaIdentityAndPayload(t *testing.T) {
	type alternateUnit struct{}
	typ := flowSchema()
	same := flowSchema()
	otherPayload := schema.Define[flowUnitID, alternateUnit](schema.Traits[alternateUnit]{})
	left := NewShape(nil, []Port{Out("out", typ)})
	if !left.Equal(NewShape(nil, []Port{Out("out", same)})) {
		t.Fatal("equivalent schema declarations produced different shapes")
	}
	if left.Equal(NewShape(nil, []Port{Out("out", otherPayload)})) {
		t.Fatal("same schema marker with a different payload produced an equal shape")
	}
}

func countingSchema(drops *atomic.Int32) schema.Type[int] {
	return schema.Define[flowValueID, int](schema.Traits[int]{
		Fork: func(value int) int { return value },
		Drop: func(int) { drops.Add(1) },
	})
}

// A deferred Drop is the whole ownership rule, so it must stay correct when
// something downstream has already consumed the cell.
func TestDeferredDropReleasesOnceWhetherOrNotSomethingConsumedTheCell(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)

	moved := func() {
		item := NewItem(1, typ)
		defer item.Drop()
		var downstream Item[int]
		downstream.Move(&item)
		downstream.Drop()
	}
	moved()
	if drops.Load() != 1 {
		t.Fatalf("release count after a move = %d, want 1", drops.Load())
	}

	abandoned := func() {
		item := NewItem(2, typ)
		defer item.Drop()
	}
	abandoned()
	if drops.Load() != 2 {
		t.Fatalf("release count after an unconsumed cell = %d, want 2", drops.Load())
	}

	repeated := NewItem(3, typ)
	repeated.Drop()
	repeated.Drop()
	if drops.Load() != 3 {
		t.Fatalf("release count after repeated Drop = %d, want 3", drops.Load())
	}
}

func TestForkIsTheOnlyRetainAndEachOwnerReleasesOnce(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)
	original := NewItem(42, typ)
	var branch Item[int]
	if !original.Fork(&branch) || !original.Valid() || !branch.Valid() {
		t.Fatal("fork did not produce two independent owners")
	}
	branch.Drop()
	if drops.Load() != 1 {
		t.Fatalf("release count after dropping a branch = %d, want 1", drops.Load())
	}
	original.Drop()
	if drops.Load() != 2 {
		t.Fatalf("release count after dropping the original = %d, want 2", drops.Load())
	}
}

func TestItemCanUseThirdPartyTraitsWithoutFlowState(t *testing.T) {
	var drops atomic.Int32
	item := NewItemWithTraits(8, func(value int) int { return value + 1 }, func(int) { drops.Add(1) })
	var branch Item[int]
	item.Fork(&branch)
	if branch.Value() != 9 {
		t.Fatal("third-party fork trait was not used")
	}
	branch.Drop()
	item.Drop()
	if drops.Load() != 2 {
		t.Fatalf("drop count = %d", drops.Load())
	}
}

// Detaching ownership for a queue and taking it back must not allocate, so a
// bounded edge costs no more than a direct call.
func TestOwnershipTransferHasNoAllocation(t *testing.T) {
	allocations := testing.AllocsPerRun(1000, func() {
		item := NewItem(1, linearValueSchema)
		stored := item.Consume()
		if !stored.Valid() || item.Valid() {
			t.Fatal("consume did not move ownership out of the cell")
		}
		item.Adopt(stored)
		item.Drop()
	})
	if allocations != 0 {
		t.Fatalf("linear ownership transfer allocations = %v, want 0", allocations)
	}
}
