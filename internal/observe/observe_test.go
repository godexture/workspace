package observe

import (
	"testing"
	"time"
)

func TestOffCreatesNoCounterAndNeverReadsClock(t *testing.T) {
	reads := 0
	collector := New(Off, func() time.Time {
		reads++
		return time.Unix(1, 0)
	})
	if local := collector.Local("node", "edge"); local != nil {
		t.Fatal("Off created a local counter")
	}
	collector.Emit(Event{Kind: Lifecycle})
	if reads != 0 || len(collector.Snapshot()) != 0 {
		t.Fatalf("Off = clock reads %d, events %d", reads, len(collector.Snapshot()))
	}
}

func TestBasicBatchesWithoutClockOrDetailedValues(t *testing.T) {
	reads := 0
	collector := New(Basic, func() time.Time {
		reads++
		return time.Unix(1, 0)
	})
	local := collector.Local("node", "edge")
	local.Add(100, 4, true)
	local.Add(200, 8, true)
	local.Flush()
	events := collector.Snapshot()
	if reads != 0 || len(events) != 1 || events[0].Items != 2 || events[0].Bytes != 0 || events[0].HasMedia || !events[0].At.IsZero() {
		t.Fatalf("Basic events = %#v, clock reads %d", events, reads)
	}
}

func TestDetailedSnapshotsAreImmutable(t *testing.T) {
	now := time.Unix(10, 0)
	collector := New(Detailed, func() time.Time {
		now = now.Add(time.Second)
		return now
	})
	local := collector.Local("node", "edge")
	local.Add(12, 9, true)
	local.Flush()
	collector.Emit(Event{Kind: Diagnostic, Detail: map[string]string{"key": "value"}})
	first := collector.Snapshot()
	first[1].Detail["key"] = "changed"
	second := collector.Snapshot()
	if len(second) != 2 || second[0].Bytes != 12 || second[0].Media != 9 || !second[0].HasMedia || second[1].Detail["key"] != "value" {
		t.Fatalf("Detailed snapshot = %#v", second)
	}
}

func TestTraceEmitsPerItemAndAggregate(t *testing.T) {
	collector := New(Trace, func() time.Time { return time.Unix(1, 0) })
	local := collector.Local("node", "edge")
	local.Add(3, 7, true)
	local.Flush()
	events := collector.Snapshot()
	if len(events) != 2 || events[0].Items != 1 || events[1].Items != 1 {
		t.Fatalf("Trace events = %#v", events)
	}
}
