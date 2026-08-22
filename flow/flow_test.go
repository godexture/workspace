package flow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/internal/ownership"
	"github.com/godexture/godec/media/schema"
)

type flowUnitID struct{}
type flowUnit struct{ Value int }
type flowValueID struct{}
type alternateValueID struct{}
type panicDropID struct{}
type thirdPartyDropID struct{}

type ownershipAuditDomain struct {
	deltas []int64
	live   int64
	panic  bool
}

func (*ownershipAuditDomain) Cleanup(error) {}

func (d *ownershipAuditDomain) TrackFlowOwnership(delta int64) {
	d.track(delta)
}

func (d *ownershipAuditDomain) track(delta int64) {
	if d.panic {
		panic("ownership audit failed")
	}
	d.deltas = append(d.deltas, delta)
	d.live += delta
}

func auditedReporter(d *ownershipAuditDomain) Reporter {
	return ownership.Wrap(d, d.track)
}

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

// TestDirectPortRequiresSerialFanIn keeps the option attached to the only
// policy whose meaning it sharpens. Every other policy either combines its
// inputs itself or owns queues that decouple them on purpose, so requiring one
// producer's emit order there would be a contradiction rather than a promise.
func TestDirectPortRequiresSerialFanIn(t *testing.T) {
	typ := flowSchema()
	direct := In("packets", typ, Many(), WithFanIn(SerialFanIn), Direct())
	if err := NewShape([]Port{direct}, []Port{Out("writes", typ)}).Validate(); err != nil {
		t.Fatalf("direct serial fan-in port rejected: %v", err)
	}
	if !direct.Direct() {
		t.Fatal("direct port did not report its requirement")
	}
	for name, port := range map[string]Port{
		"zip":        In("zip", typ, Many(), WithFanIn(ZipFanIn), Direct()),
		"one input":  In("one", typ, Direct()),
		"one output": Out("out", typ, Direct()),
	} {
		t.Run(name, func(t *testing.T) {
			shape := NewShape([]Port{port}, []Port{Out("sink", typ)})
			if port.Direction() == OutputDirection {
				shape = NewShape([]Port{In("source", typ)}, []Port{port})
			}
			if err := shape.Validate(); err == nil {
				t.Fatal("direct requirement accepted without serial fan-in")
			}
		})
	}
	// Two shapes that differ only in the requirement are different contracts,
	// so a component cannot be swapped for one that drops it.
	plain := In("packets", typ, Many(), WithFanIn(SerialFanIn))
	if NewShape([]Port{direct}, nil).Equal(NewShape([]Port{plain}, nil)) {
		t.Fatal("a direct port compared equal to one without the requirement")
	}
}

func TestSelectedBatchKeepsItsInputWithoutChangingZipBatches(t *testing.T) {
	typ := flowSchema()
	item := NewItem(flowUnit{Value: 3}, typ, &testDomain)
	defer item.Drop()
	selected := NewSelectedBatch(4, &item)
	if input, ok := selected.Input(); !ok || input != 4 {
		t.Fatalf("selected input = %d, %v", input, ok)
	}
	if selected.Len() != 1 || selected.At(0) != &item || selected.At(1) != nil {
		t.Fatalf("selected batch = len %d, first %p, second %p", selected.Len(), selected.At(0), selected.At(1))
	}
	if value, ok := selected.Value(0); !ok || value.Value != 3 {
		t.Fatalf("selected value = %#v, %v", value, ok)
	}
	zip := NewBatch([]*Item[flowUnit]{&item})
	if _, ok := zip.Input(); ok {
		t.Fatal("zip batch unexpectedly selected an input")
	}
	if zip.Len() != 1 || zip.At(0) != &item {
		t.Fatalf("zip batch changed = len %d, item %p", zip.Len(), zip.At(0))
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
	timed := schema.Define[flowUnitID, flowUnit](schema.Traits[flowUnit]{Time: func(flowUnit) (int64, bool) { return 0, true }})
	if left.Equal(NewShape(nil, []Port{Out("out", timed)})) {
		t.Fatal("same schema marker with a different time-trait presence produced an equal shape")
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
		item := NewItem(1, typ, &testDomain)
		defer item.Drop()
		var downstream Item[int]
		downstream.Bind(typ, &testDomain)
		if !downstream.Move(&item) || item.Valid() || !downstream.Valid() {
			t.Fatal("downstream did not consume the source slot")
		}
		downstream.Drop()
	}
	moved()
	if drops.Load() != 1 {
		t.Fatalf("release count after a move = %d, want 1", drops.Load())
	}

	abandoned := func() {
		item := NewItem(2, typ, &testDomain)
		defer item.Drop()
	}
	abandoned()
	if drops.Load() != 2 {
		t.Fatalf("release count after an unconsumed cell = %d, want 2", drops.Load())
	}

	repeated := NewItem(3, typ, &testDomain)
	repeated.Drop()
	repeated.Drop()
	if drops.Load() != 3 {
		t.Fatalf("release count after repeated Drop = %d, want 3", drops.Load())
	}
}

func TestForkIsTheOnlyRetainAndEachOwnerReleasesOnce(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)
	original := NewItem(42, typ, &testDomain)
	var branch Item[int]
	branch.Bind(typ, &testDomain)
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

func TestForkRejectsSelfTargetWithoutReleasingOrReplacingSource(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)
	item := NewItem(42, typ, &testDomain)
	if item.Fork(&item) {
		t.Fatal("self-target Fork succeeded")
	}
	if !item.Valid() || item.Value() != 42 {
		t.Fatalf("self-target Fork changed source: valid=%v value=%d", item.Valid(), item.Value())
	}
	if drops.Load() != 0 {
		t.Fatalf("self-target Fork released source %d time(s), want zero", drops.Load())
	}
	item.Drop()
	if drops.Load() != 1 {
		t.Fatalf("source release count = %d, want one", drops.Load())
	}
}

func TestItemCanUseThirdPartyTraitsWithoutFlowState(t *testing.T) {
	var drops atomic.Int32
	thirdParty := schema.Define[thirdPartyDropID](schema.Traits[int]{
		Fork: func(value int) int { return value + 1 },
		Drop: func(int) { drops.Add(1) },
	})
	item := NewItem(8, thirdParty, &testDomain)
	var branch Item[int]
	branch.Bind(thirdParty, &testDomain)
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
		item := NewItem(1, linearValueSchema, &testDomain)
		var stored Item[int]
		stored.Bind(linearValueSchema, &testDomain)
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

func TestItemReportsEveryCommittedOwnershipTransition(t *testing.T) {
	var sourceAudit, targetAudit ownershipAuditDomain
	var liveAtDrop int64
	typ := schema.Define[flowValueID, int](schema.Traits[int]{
		Fork: func(value int) int { return value },
		Drop: func(int) { liveAtDrop = targetAudit.live },
	})

	source := NewItem(1, typ, auditedReporter(&sourceAudit))
	var target Item[int]
	target.Bind(typ, auditedReporter(&targetAudit))
	if !target.Move(&source) {
		t.Fatal("Move was refused")
	}
	if got, want := sourceAudit.deltas, []int64{1, -1}; !slicesEqual(got, want) {
		t.Fatalf("source deltas = %v, want %v", got, want)
	}
	if got, want := targetAudit.deltas, []int64{1}; !slicesEqual(got, want) {
		t.Fatalf("target deltas = %v, want %v", got, want)
	}

	var branch Item[int]
	branch.Bind(typ, auditedReporter(&targetAudit))
	if !target.Fork(&branch) {
		t.Fatal("Fork was refused")
	}
	if target.Fork(&Item[int]{}) || target.Fork(&target) {
		t.Fatal("Fork refusal semantics changed")
	}
	branch.Drop()
	if liveAtDrop != 1 {
		t.Fatalf("audit live count at Drop callback = %d, want 1: the slot delta must be applied first", liveAtDrop)
	}
	target.Drop()
	if sourceAudit.live != 0 || targetAudit.live != 0 {
		t.Fatalf("final audit counts = source %d target %d", sourceAudit.live, targetAudit.live)
	}
}

func TestTransferOwnershipAuditBalancesSuccessErrorAndPanic(t *testing.T) {
	typ := schema.Define[flowValueID, int](schema.Traits[int]{})

	t.Run("success", func(t *testing.T) {
		var sourceAudit, targetAudit ownershipAuditDomain
		source := NewItem(1, typ, auditedReporter(&sourceAudit))
		var target Item[int]
		edge := auditedEdge[int]{typ: typ, reporter: auditedReporter(&targetAudit)}
		if err := Transfer(&source, &target, edge, func(value int) (int, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		target.Drop()
		if sourceAudit.live != 0 || targetAudit.live != 0 {
			t.Fatalf("counts = source %d target %d", sourceAudit.live, targetAudit.live)
		}
	})

	t.Run("error", func(t *testing.T) {
		var sourceAudit, targetAudit ownershipAuditDomain
		source := NewItem(1, typ, auditedReporter(&sourceAudit))
		var target Item[int]
		err := Transfer(&source, &target, auditedEdge[int]{typ: typ, reporter: auditedReporter(&targetAudit)}, func(int) (int, error) {
			return 0, errTransferTest
		})
		if !errors.Is(err, errTransferTest) || sourceAudit.live != 0 || targetAudit.live != 0 {
			t.Fatalf("error = %v, counts = source %d target %d", err, sourceAudit.live, targetAudit.live)
		}
	})

	t.Run("panic", func(t *testing.T) {
		var sourceAudit, targetAudit ownershipAuditDomain
		func() {
			defer func() { _ = recover() }()
			source := NewItem(1, typ, auditedReporter(&sourceAudit))
			var target Item[int]
			_ = Transfer(&source, &target, auditedEdge[int]{typ: typ, reporter: auditedReporter(&targetAudit)}, func(int) (int, error) {
				panic("build")
			})
		}()
		if sourceAudit.live != 0 || targetAudit.live != 0 {
			t.Fatalf("counts = source %d target %d", sourceAudit.live, targetAudit.live)
		}
	})
}

func TestPanickingOwnershipAuditDoesNotReplaceItemSemantics(t *testing.T) {
	var drops atomic.Int32
	domain := ownershipAuditDomain{panic: true}
	typ := countingSchema(&drops)
	item := NewItem(1, typ, ownership.Wrap(&domain, func(int64) { panic("ownership audit failed") }))
	item.Drop()
	if item.Valid() || drops.Load() != 1 {
		t.Fatalf("item valid = %v, drops = %d", item.Valid(), drops.Load())
	}
}

func TestReporterWithSimilarExportedHookDoesNotOptIntoAudit(t *testing.T) {
	var domain ownershipAuditDomain
	item := NewItem(1, linearValueSchema, &domain)
	item.Drop()
	if len(domain.deltas) != 0 || domain.live != 0 {
		t.Fatalf("unwrapped Reporter received ownership callbacks: deltas=%v live=%d", domain.deltas, domain.live)
	}
}

func TestBindKeepsFirstSuccessfulDeclarationForReusableSlot(t *testing.T) {
	var firstDrops, secondDrops atomic.Int32
	firstType := schema.Define[flowValueID, int](schema.Traits[int]{
		Drop: func(int) { firstDrops.Add(1) },
	})
	secondType := schema.Define[alternateValueID, int](schema.Traits[int]{
		Drop: func(int) { secondDrops.Add(1) },
	})
	var firstDomain, secondDomain ownershipAuditDomain
	first := auditedEdge[int]{typ: firstType, reporter: auditedReporter(&firstDomain)}
	second := auditedEdge[int]{typ: secondType, reporter: auditedReporter(&secondDomain)}

	var item Item[int]
	first.Own(&item, 1)
	item.Drop()
	second.Own(&item, 2)
	item.Drop()

	if firstDrops.Load() != 2 || secondDrops.Load() != 0 {
		t.Fatalf("drop counts after rebinding = first %d, second %d; want first 2, second 0", firstDrops.Load(), secondDrops.Load())
	}
	if firstDomain.live != 0 || !slicesEqual(firstDomain.deltas, []int64{1, -1, 1, -1}) {
		t.Fatalf("first domain audit = live %d deltas %v", firstDomain.live, firstDomain.deltas)
	}
	if secondDomain.live != 0 || len(secondDomain.deltas) != 0 {
		t.Fatalf("second domain was used after a rejected rebind: live %d deltas %v", secondDomain.live, secondDomain.deltas)
	}
}

func TestBindWithNilReporterDoesNotDeclareAnUnboundSlot(t *testing.T) {
	var item Item[int]
	item.Bind(linearValueSchema, nil)
	if item.Bound() {
		t.Fatal("nil reporter declared an ownership slot")
	}
	item.Bind(linearValueSchema, &testDomain)
	if !item.Bound() {
		t.Fatal("a valid declaration after a rejected nil reporter was ignored")
	}
	item.Set(1)
	item.Drop()
}

// Copying an Item is forbidden and go vet normally rejects it. Reflection is
// used only to reproduce a broken plugin binary and prove the runtime audit
// still reports the second release as a negative balance.
func TestOwnershipAuditDetectsCopiedSlotOverrelease(t *testing.T) {
	var domain ownershipAuditDomain
	item := NewItem(1, linearValueSchema, auditedReporter(&domain))
	copyValue := reflect.New(reflect.TypeOf((*Item[int])(nil)).Elem())
	copyValue.Elem().Set(reflect.ValueOf(&item).Elem())
	copy := copyValue.Interface().(*Item[int])
	item.Drop()
	copy.Drop()
	if domain.live != -1 || !slicesEqual(domain.deltas, []int64{1, -1, -1}) {
		t.Fatalf("copy audit = live %d deltas %v", domain.live, domain.deltas)
	}
}

func slicesEqual(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type auditedEdge[T any] struct {
	typ      schema.Type[T]
	reporter Reporter
}

func (e auditedEdge[T]) Own(into *Item[T], value T) {
	into.Bind(e.typ, e.reporter)
	into.Set(value)
}

func (auditedEdge[T]) Emit(context.Context, *Item[T]) error { return nil }

// Transfer empties the source before running a conversion it does not control,
// so every way out of that conversion must release the value exactly once.
func TestTransferReleasesExactlyOnceOnEveryPath(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)

	t.Run("success hands the value to build", func(t *testing.T) {
		drops.Store(0)
		source := NewItem(1, typ, &testDomain)
		defer source.Drop()
		var target Item[int]
		if err := Transfer(&source, &target, edge[int]{typ}, func(value int) (int, error) { return value, nil }); err != nil {
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
		source := NewItem(2, typ, &testDomain)
		defer source.Drop()
		var target Item[int]
		defer target.Drop()
		if err := Transfer(&source, &target, edge[int]{typ}, func(int) (int, error) { return 0, errTransferTest }); !errors.Is(err, errTransferTest) {
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
			source := NewItem(3, typ, &testDomain)
			defer source.Drop()
			var target Item[int]
			defer target.Drop()
			_ = Transfer(&source, &target, edge[int]{typ}, func(int) (int, error) { panic("build failed") })
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
		target := NewItem(4, typ, &testDomain)
		defer target.Drop()
		source := NewItem(5, typ, &testDomain)
		defer source.Drop()
		if err := Transfer(&source, &target, edge[int]{typ}, func(value int) (int, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		if drops.Load() != 1 {
			t.Fatalf("release count after overwriting the target = %d, want 1", drops.Load())
		}
	})

	// The source slot is already empty when the target releases what it held, so
	// a failure there must not take the converted payload with it: the target
	// ends up holding it, and the failure goes to the domain.
	t.Run("a failed target release loses nothing", func(t *testing.T) {
		var panicking atomic.Int32
		panickingType := schema.Define[panicDropID](schema.Traits[int]{
			Drop: func(int) {
				panicking.Add(1)
				panic("declared drop panicked")
			},
		})
		var domain Collector
		target := NewItem(6, panickingType, &domain)
		source := NewItem(7, panickingType, &domain)
		if err := Transfer(&source, &target, edge[int]{panickingType}, func(value int) (int, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		if !target.Valid() || target.Value() != 7 {
			t.Fatal("the converted payload was lost by the release it replaced")
		}
		if panicking.Load() != 1 {
			t.Fatalf("release count = %d, want the payload the target held", panicking.Load())
		}
		if got := len(domain.Failures()); got != 1 {
			t.Fatalf("failures reported to the domain = %d, want the release that could not finish", got)
		}
		target.Drop()
	})
}

// A container cannot hold cells by value, so a collector holds pointers to
// them and moves each payload into its own cell. That keeps the release
// obligation in exactly one place per payload with no second representation of
// ownership to copy.
func TestCellsMoveIntoAContainerWithoutASecondOwner(t *testing.T) {
	var drops atomic.Int32
	typ := countingSchema(&drops)

	var collected []*Item[int]
	for value := 1; value <= 3; value++ {
		emitted := NewItem(value, typ, &testDomain)
		stored := new(Item[int])
		stored.Bind(typ, &testDomain)
		stored.Move(&emitted)
		if emitted.Valid() || !stored.Valid() {
			t.Fatal("moving into a container cell left two owners")
		}
		collected = append(collected, stored)
	}
	if drops.Load() != 0 {
		t.Fatalf("release count while collected = %d, want 0", drops.Load())
	}
	for _, stored := range collected {
		stored.Drop()
		stored.Drop()
	}
	if drops.Load() != 3 {
		t.Fatalf("release count after draining the container = %d, want 3", drops.Load())
	}
}

// A declared Drop is third-party code, and a slot that takes a payload releases
// what it held first. That release decides nothing any more: it cannot raise,
// so the incoming payload is stored either way, and the failure goes to the
// domain the slot belongs to instead of unwinding through the caller.
func TestTakingOverAFailedReleaseKeepsTheIncomingPayload(t *testing.T) {
	var drops atomic.Int32
	panicking := schema.Define[panicDropID](schema.Traits[int]{
		Drop: func(int) {
			drops.Add(1)
			panic("declared drop panicked")
		},
	})

	assertKept := func(t *testing.T, hand func(target *Item[int])) {
		t.Helper()
		drops.Store(0)
		var domain Collector
		target := NewItem(1, panicking, &domain)
		hand(&target)
		if !target.Valid() || target.Value() != 2 {
			t.Error("the incoming payload was lost by the release it replaced")
		}
		if got := len(domain.Failures()); got != 1 {
			t.Errorf("failures reported to the domain = %d, want the release that could not finish", got)
		}
		target.Drop()
	}

	t.Run("Set", func(t *testing.T) {
		assertKept(t, func(target *Item[int]) { target.Set(2) })
	})
	t.Run("Fork", func(t *testing.T) {
		assertKept(t, func(target *Item[int]) {
			var source Collector
			value := NewItem(2, panicking, &source)
			value.Fork(target)
			value.Drop()
		})
	})
}

var errTransferTest = errors.New("transfer build failure")

// A domain is third-party code as much as a declared Drop is, and reporting to
// one is the last step of a release. A domain that panics must not take the
// release path with it, because that path runs where a failure has already
// stopped the work.
func TestReportingToAPanickingDomainDoesNotRaise(t *testing.T) {
	var released atomic.Int32
	panicking := schema.Define[panicDropID](schema.Traits[int]{
		Drop: func(int) {
			released.Add(1)
			panic("declared drop panicked")
		},
	})
	item := NewItem(1, panicking, panickingDomain{})
	item.Drop()
	if released.Load() != 1 {
		t.Fatalf("release attempts = %d", released.Load())
	}
	if item.Valid() {
		t.Fatal("the slot kept a payload it had released")
	}
}

// An unbound slot knows no release and no domain, so taking ownership there
// would lose the payload with no diagnosis. It is a programming error and says
// so, rather than returning as though the hand-off had happened.
func TestOwnershipHandedToAnUnboundSlotIsRefusedLoudly(t *testing.T) {
	recovered := func() (value any) {
		defer func() { value = recover() }()
		var slot Item[int]
		slot.Set(7)
		return nil
	}()
	if recovered == nil {
		t.Fatal("an unbound slot accepted ownership silently")
	}
}

type panickingDomain struct{}

func (panickingDomain) Cleanup(error) { panic("the domain panicked") }

// A slot that is not there is as unbound as one that was never declared, and
// the payload is lost either way. Saying so is the only difference available.
func TestOwnershipHandedToAnAbsentSlotIsRefusedLoudly(t *testing.T) {
	recovered := func() (value any) {
		defer func() { value = recover() }()
		var slot *Item[int]
		slot.Set(7)
		return nil
	}()
	if recovered == nil {
		t.Fatal("an absent slot accepted ownership silently")
	}
}
