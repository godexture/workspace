package flow

import (
	"testing"
	"unsafe"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type perfSlotID struct{}

// A bounded ring holds Items contiguously, so what an Item costs is what a
// queue costs per slot. The failure domain an Item carries is one interface
// word pair pointing at a Site that already exists; the ledger, the domain's
// identity, the task name, the node and the event identity all live on the
// other side of that pointer and never in the slot.
//
// This records the size rather than asserting an exact number for every
// payload. The two declared traits, domain handle, and compact slot flags fit
// in the same 48-byte Item[int] layout with or without audit support.
func TestItemCarriesOnlyAPayloadAndADomainHandle(t *testing.T) {
	type empty struct{}
	// The whole of what a slot keeps around a payload: the two declared traits,
	// one handle to the failure domain, and the compact flags.
	type accepted struct {
		fork     func(empty) empty
		drop     func(empty)
		reporter Reporter
		bound    bool
		valid    bool
		audited  bool
	}
	overhead := unsafe.Sizeof(Item[empty]{})
	if want := unsafe.Sizeof(accepted{}); overhead != want {
		t.Fatalf("Item ownership overhead = %d bytes, want %d: the slot must not carry ledger, span, task, node or event state", overhead, want)
	}
	if size := unsafe.Sizeof(Item[int]{}); size != 48 {
		t.Fatalf("Item[int] = %d bytes, want 48", size)
	}
	t.Logf("Item[struct{}] = %d bytes, Item[int] = %d bytes", overhead, unsafe.Sizeof(Item[int]{}))
}

// A release that succeeds touches nothing shared. It runs the declared Drop
// and returns; the domain is not told, so no lock is taken, no event is
// numbered, and nothing is appended to a run's evidence.
func TestASuccessfulReleaseNeverReachesTheDomain(t *testing.T) {
	released := 0
	typ := schema.Define[perfSlotID](schema.Traits[int]{Drop: func(int) { released++ }})
	var domain Collector
	allocations := testing.AllocsPerRun(1000, func() {
		item := NewItem(1, typ, &domain)
		item.Drop()
	})
	if allocations != 0 {
		t.Fatalf("successful release allocations = %v, want 0", allocations)
	}
	if len(domain.Failures()) != 0 {
		t.Fatalf("a successful release reported %d failures to its domain", len(domain.Failures()))
	}
	if released == 0 {
		t.Fatal("the declared release never ran")
	}
}

// Binding a slot to a domain costs nothing per slot: the reporter is a handle
// the runtime already holds, not a value the slot builds.
func TestBindingADomainDoesNotAllocate(t *testing.T) {
	typ := schema.Define[perfSlotID](schema.Traits[int]{})
	var domain Collector
	var slot Item[int]
	allocations := testing.AllocsPerRun(1000, func() {
		slot.Bind(typ, &domain)
		slot.Set(1)
		slot.Drop()
	})
	if allocations != 0 {
		t.Fatalf("bind/set/drop allocations = %v, want 0", allocations)
	}
}

func TestDisabledRuntimeOwnershipAuditDoesNotAllocate(t *testing.T) {
	typ := schema.Define[perfSlotID](schema.Traits[int]{})
	ledger := journal.NewLedger()
	domain := ledger.Domain("allocation", "node").At("node")
	var slot Item[int]
	slot.Bind(typ, domain)
	allocations := testing.AllocsPerRun(1000, func() {
		slot.Set(1)
		slot.Drop()
	})
	if allocations != 0 {
		t.Fatalf("disabled runtime ownership audit allocations = %v, want 0", allocations)
	}
}

func BenchmarkItemMoveAndDrop(b *testing.B) {
	typ := schema.Define[perfSlotID](schema.Traits[int]{Drop: func(int) {}})
	ledger := journal.NewLedger()
	domain := ledger.Domain("benchmark", "node").At("node")
	var source, target Item[int]
	source.Bind(typ, domain)
	target.Bind(typ, domain)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		source.Set(1)
		target.Move(&source)
		target.Drop()
	}
}

func BenchmarkItemMoveAndDropAudited(b *testing.B) {
	typ := schema.Define[perfSlotID](schema.Traits[int]{Drop: func(int) {}})
	ledger := journal.NewLedger()
	ledger.EnableOwnershipAudit()
	domain := ledger.Domain("benchmark", "node").At("node").Reporter()
	var source, target Item[int]
	source.Bind(typ, domain)
	target.Bind(typ, domain)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		source.Set(1)
		target.Move(&source)
		target.Drop()
	}
}
