package release

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type panicDropID struct{}

// A group of owners is released together. One release that panics must not
// strand the owners behind it, and cleanup runs where no recovery boundary is
// left, so the failures are reported rather than raised.
func TestAllReleasesEveryCellAndReportsTheFailures(t *testing.T) {
	var released atomic.Int32
	panicking := schema.Define[panicDropID](schema.Traits[int]{
		Drop: func(value int) {
			released.Add(1)
			if value%2 == 0 {
				panic("declared drop panicked")
			}
		},
	})
	cells := make([]flow.Item[int], 4)
	for index := range cells {
		cells[index].Set(index, panicking)
	}

	err := All(cells)
	if err == nil {
		t.Fatal("a panicking release was not reported")
	}
	if released.Load() != 4 {
		t.Fatalf("released cells = %d, want every cell released", released.Load())
	}
	if strings.Contains(err.Error(), "declared drop panicked") {
		t.Error("the release report exposes the recovered panic value")
	}
	for index := range cells {
		if cells[index].Valid() {
			t.Fatalf("cell %d still holds a payload", index)
		}
	}
	if err := All(cells); err != nil {
		t.Fatalf("releasing empty cells reported %v", err)
	}
	if released.Load() != 4 {
		t.Fatal("an already released cell was released again")
	}
}
