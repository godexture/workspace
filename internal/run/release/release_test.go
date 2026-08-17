package release

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type panicDropID struct{}

// A group of owners is released together. One release that fails must not
// strand the owners behind it, and each slot answers for its own failure, so
// the group needs no report of its own.
func TestAllReleasesEverySlotAndLeavesTheReportingToThem(t *testing.T) {
	var released atomic.Int32
	panicking := schema.Define[panicDropID](schema.Traits[int]{
		Drop: func(value int) {
			released.Add(1)
			if value%2 == 0 {
				panic("declared drop panicked")
			}
		},
	})
	var domain flow.Collector
	slots := make([]flow.Item[int], 4)
	for index := range slots {
		slots[index].Bind(panicking, &domain)
		slots[index].Set(index)
	}

	All(slots)
	if released.Load() != 4 {
		t.Fatalf("released slots = %d, want every slot released", released.Load())
	}
	failures := domain.Failures()
	if len(failures) != 2 {
		t.Fatalf("failures reported to the domain = %d, want one per release that could not finish", len(failures))
	}
	for _, failure := range failures {
		if strings.Contains(failure.Error(), "declared drop panicked") {
			t.Error("the release report exposes the recovered panic value")
		}
	}
	for index := range slots {
		if slots[index].Valid() {
			t.Fatalf("slot %d still holds a payload", index)
		}
	}

	All(slots)
	if released.Load() != 4 {
		t.Fatal("an already released slot was released again")
	}
	if len(domain.Failures()) != 2 {
		t.Fatal("releasing empty slots reported a failure")
	}
}
