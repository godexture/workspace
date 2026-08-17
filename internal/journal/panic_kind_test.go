package journal

import (
	"errors"
	"testing"
)

func TestLedgerClassifiesPanicPerJoinedOccurrence(t *testing.T) {
	for _, test := range []struct {
		name      string
		base      Kind
		wantError Kind
		wantPanic Kind
	}{
		{name: "work", base: WorkError, wantError: WorkError, wantPanic: WorkPanic},
		{name: "cleanup", base: CleanupError, wantError: CleanupError, wantPanic: CleanupPanic},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger := NewLedger()
			ordinary := errors.New("ordinary child failure")
			panicked := &PanicError{Name: "child-task", Location: "panic-child", Summary: "boom", Stack: []byte("stack")}
			ledger.Record(Entry{
				Kind:      test.base,
				Operation: Close,
				Task:      "composite",
				Node:      "parent",
				Err:       errors.Join(ordinary, panicked),
			})
			events := ledger.Events()
			if len(events) != 2 {
				t.Fatalf("events = %#v", events)
			}
			if events[0].Kind != test.wantError || events[0].Node != "parent" || events[0].Task != "composite" || !errors.Is(events[0].Err, ordinary) {
				t.Fatalf("ordinary event = %#v", events[0])
			}
			if events[1].Kind != test.wantPanic || events[1].Node != "panic-child" || events[1].Task != "child-task" || string(events[1].Stack) != "stack" {
				t.Fatalf("panic event = %#v", events[1])
			}
		})
	}
}
