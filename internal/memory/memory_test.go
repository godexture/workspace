package memory

import (
	"errors"
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/resource"
)

func TestReservationsAreCoarseAndRepaidOnce(t *testing.T) {
	manager := New(resource.Grant{Memory: 32, Workers: 2, Queue: 4})
	lease, err := manager.Reserve("decoder", resource.Request{Memory: 16, Workers: 1, Queue: 2})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := lease.Buffers().Allocate(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	if before.Used.Memory != 16 || before.Used.Workers != 1 || len(before.Active) != 1 {
		t.Fatalf("reservation snapshot = %#v", before)
	}
	handle.Release()
	afterItem := manager.Snapshot()
	if afterItem.Used != before.Used {
		t.Fatalf("item accounting reached manager: before %#v after %#v", before.Used, afterItem.Used)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if used := manager.Snapshot().Used; used != (resource.Grant{}) {
		t.Fatalf("released grant = %#v", used)
	}
}

func TestReservationRejectsEachExhaustedDimension(t *testing.T) {
	tests := []resource.Request{
		{Memory: 2},
		{Temporary: 2},
		{Workers: 2},
		{Queue: 2},
	}
	for _, request := range tests {
		manager := New(resource.Grant{Memory: 1, Temporary: 1, Workers: 1, Queue: 1})
		if _, err := manager.Reserve("too-large", request); !errors.Is(err, ErrExhausted) {
			t.Fatalf("request %#v error = %v", request, err)
		}
	}
}

func TestCloseReportsLiveReservationsAndRejectsNewOnes(t *testing.T) {
	manager := New(resource.Grant{Memory: 4})
	lease, err := manager.Reserve("live", resource.Request{Memory: 4})
	if err != nil {
		t.Fatal(err)
	}
	report := manager.Close()
	if len(report.Active) != 1 || report.Active[0].Name != "live" {
		t.Fatalf("close report = %#v", report)
	}
	if _, err := manager.Reserve("late", resource.Request{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("late reservation = %v", err)
	}
	_ = lease.Close()
	if used := manager.Snapshot().Used; used != (resource.Grant{}) {
		t.Fatalf("late release = %#v", used)
	}
}
