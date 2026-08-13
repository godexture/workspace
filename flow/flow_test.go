package flow

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/media/schema"
)

type flowUnitID struct{}
type flowUnit struct{ Value int }
type flowValueID struct{}
type panicDropID struct{}

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

// Handing a payload to a bounded edge and taking it back is a move between
// cells, so it must not allocate: an edge costs no more than a direct call.
func TestOwnershipTransferHasNoAllocation(t *testing.T) {
	allocations := testing.AllocsPerRun(1000, func() {
		item := NewItem(1, linearValueSchema)
		var stored Item[int]
		stored.Move(&item)
		if item.Valid() || !stored.Valid() {
			t.Fatal("move did not transfer the payload between cells")
		}
		item.Move(&stored)
		item.Drop()
	})
	if allocations != 0 {
		t.Fatalf("linear ownership transfer allocations = %v, want 0", allocations)
	}
}

// Transfer empties the source before running a conversion it does not control,
// so every way out of that conversion must release the value exactly once.
func TestTransferReleasesExactlyOnceOnEveryPath(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)

	t.Run("success hands the value to build", func(t *testing.T) {
		drops.Store(0)
		source := NewItem(1, typ)
		defer source.Drop()
		var target Item[int]
		if err := Transfer(&source, &target, typ, func(value int) (int, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		if drops.Load() != 0 {
			t.Fatalf("release count after a successful transfer = %d, want 0", drops.Load())
		}
		target.Drop()
		if drops.Load() != 1 {
			t.Fatalf("release count after dropping the target = %d, want 1", drops.Load())
		}
	})

	t.Run("returned error releases once", func(t *testing.T) {
		drops.Store(0)
		source := NewItem(2, typ)
		defer source.Drop()
		var target Item[int]
		defer target.Drop()
		if err := Transfer(&source, &target, typ, func(int) (int, error) { return 0, errTransferTest }); !errors.Is(err, errTransferTest) {
			t.Fatalf("transfer error = %v", err)
		}
		if drops.Load() != 1 || source.Valid() || target.Valid() {
			t.Fatalf("release count after a failed build = %d, want 1", drops.Load())
		}
	})

	t.Run("panic releases once and propagates", func(t *testing.T) {
		drops.Store(0)
		recovered := func() (value any) {
			defer func() { value = recover() }()
			source := NewItem(3, typ)
			defer source.Drop()
			var target Item[int]
			defer target.Drop()
			_ = Transfer(&source, &target, typ, func(int) (int, error) { panic("build failed") })
			return nil
		}()
		if recovered != "build failed" {
			t.Fatalf("recovered = %v, want the original panic value", recovered)
		}
		if drops.Load() != 1 {
			t.Fatalf("release count after a panicking build = %d, want 1", drops.Load())
		}
	})

	t.Run("target overwrite releases what it held", func(t *testing.T) {
		drops.Store(0)
		target := NewItem(4, typ)
		defer target.Drop()
		source := NewItem(5, typ)
		defer source.Drop()
		if err := Transfer(&source, &target, typ, func(value int) (int, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		if drops.Load() != 1 {
			t.Fatalf("release count after overwriting the target = %d, want 1", drops.Load())
		}
	})
}

// Detach is the only way a payload leaves a cell, and it leaves nothing behind
// that could release the value a second time.
func TestDetachLeavesNoSecondReleaser(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)
	item := NewItem(1, typ)
	parcel, ok := item.Detach()
	if !ok {
		t.Fatal("detach reported no value")
	}
	item.Drop()
	item.Drop()
	if drops.Load() != 0 {
		t.Fatalf("release count after detaching = %d, want 0", drops.Load())
	}
	var adopted Item[int]
	if !parcel.Adopt(&adopted) {
		t.Fatal("adopt refused a waiting parcel")
	}
	adopted.Drop()
	adopted.Drop()
	if drops.Load() != 1 {
		t.Fatalf("release count after the adopting cell dropped = %d, want 1", drops.Load())
	}
}

// A parcel has to be copyable, because a collector holds it by value. Copying
// it must therefore copy a handle to one state rather than a second owner: the
// first Adopt or Release wins, and no copy can release the payload again.
func TestParcelCopiesShareOneConsumption(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)

	t.Run("only one copy adopts", func(t *testing.T) {
		drops.Store(0)
		item := NewItem(1, typ)
		parcel, _ := item.Detach()
		first, second := parcel, parcel
		var left, right Item[int]
		if !first.Adopt(&left) {
			t.Fatal("the first copy did not adopt")
		}
		if second.Adopt(&right) || right.Valid() {
			t.Fatal("a copied parcel adopted the payload a second time")
		}
		left.Drop()
		right.Drop()
		if drops.Load() != 1 {
			t.Fatalf("release count = %d, want 1", drops.Load())
		}
	})

	t.Run("release after adopt does nothing", func(t *testing.T) {
		drops.Store(0)
		item := NewItem(1, typ)
		parcel, _ := item.Detach()
		copied := parcel
		var adopted Item[int]
		parcel.Adopt(&adopted)
		copied.Release()
		copied.Release()
		if drops.Load() != 0 {
			t.Fatalf("release count before the adopting cell dropped = %d, want 0", drops.Load())
		}
		adopted.Drop()
		if drops.Load() != 1 {
			t.Fatalf("release count = %d, want 1", drops.Load())
		}
	})

	t.Run("an unadopted parcel releases once", func(t *testing.T) {
		drops.Store(0)
		item := NewItem(1, typ)
		parcel, _ := item.Detach()
		copied := parcel
		parcel.Release()
		copied.Release()
		if drops.Load() != 1 {
			t.Fatalf("release count = %d, want 1", drops.Load())
		}
		if parcel.Valid() || copied.Valid() {
			t.Fatal("a consumed parcel still reports a waiting payload")
		}
	})
}

// A declared Drop is third-party code. When the cell that is about to take a
// payload panics while releasing what it held, the incoming payload has no
// owner yet, so it must not be stranded by the unwind.
func TestTakingOverAPanickingReleaseDoesNotStrandTheIncomingPayload(t *testing.T) {
	var drops atomic.Int32
	panicking := schema.Define[panicDropID](schema.Traits[int]{
		Drop: func(int) {
			drops.Add(1)
			panic("declared drop panicked")
		},
	})

	assertStranded := func(t *testing.T, hand func(target *Item[int])) {
		t.Helper()
		drops.Store(0)
		target := NewItem(1, panicking)
		func() {
			defer func() {
				if recover() == nil {
					t.Error("the declared drop panic did not propagate")
				}
			}()
			hand(&target)
		}()
		if drops.Load() != 2 {
			t.Errorf("release count = %d, want the held and the incoming payload", drops.Load())
		}
	}

	t.Run("Set", func(t *testing.T) {
		assertStranded(t, func(target *Item[int]) { target.Set(2, panicking) })
	})
	t.Run("Fork", func(t *testing.T) {
		assertStranded(t, func(target *Item[int]) {
			source := NewItem(2, panicking)
			source.Fork(target)
		})
	})
	t.Run("Adopt", func(t *testing.T) {
		assertStranded(t, func(target *Item[int]) {
			source := NewItem(2, panicking)
			parcel, _ := source.Detach()
			parcel.Adopt(target)
		})
	})
}

var errTransferTest = errors.New("transfer build failure")
