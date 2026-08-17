package journal

import (
	"sync"
	"testing"

	"github.com/godexture/godec/internal/ownership"
)

func TestOwnershipAuditRecordsEachBadNodeOnce(t *testing.T) {
	ledger := NewLedger()
	ledger.EnableOwnershipAudit()
	domain := ledger.Domain("task", "home")
	trackOwnership(domain.At("leak"), 1)
	trackOwnership(domain.At("balanced"), 1)
	trackOwnership(domain.At("balanced"), -1)
	trackOwnership(domain.At("overrelease"), -1)

	ledger.RecordOwnershipFailures()
	ledger.RecordOwnershipFailures()
	events := ledger.Events()
	if len(events) != 2 || ledger.Occurrences() != 2 {
		t.Fatalf("events = %#v, occurrences = %d, want one event per bad node", events, ledger.Occurrences())
	}
	for index, want := range []struct {
		node        string
		live        int64
		overrelease uint64
	}{
		{node: "leak", live: 1},
		{node: "overrelease", live: -1, overrelease: 1},
	} {
		event := events[index]
		imbalance, ok := event.Err.(*OwnershipError)
		if !ok || event.Kind != CleanupError || event.Operation != Resource || event.Node != want.node || event.Task != "runtime/ownership" {
			t.Fatalf("event %d = %#v", index, event)
		}
		if imbalance.Live != want.live || imbalance.Overrelease != want.overrelease {
			t.Fatalf("event %d imbalance = %#v, want live=%d overrelease=%d", index, imbalance, want.live, want.overrelease)
		}
	}
}

func TestOwnershipAuditIsRaceSafeAndIgnoresBalancedNodes(t *testing.T) {
	ledger := NewLedger()
	ledger.EnableOwnershipAudit()
	site := ledger.Domain("task", "node").At("node")
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 1_000 {
				trackOwnership(site, 1)
				trackOwnership(site, -1)
			}
		}()
	}
	group.Wait()
	ledger.RecordOwnershipFailures()
	if ledger.Failed() {
		t.Fatalf("balanced concurrent audit recorded %#v", ledger.Events())
	}
}

func trackOwnership(site *Site, delta int64) {
	ownership.Track(site.Reporter(), delta)
}
