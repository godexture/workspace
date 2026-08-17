package task

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/plugin"
)

func TestStarterEnforcesAndRepaysWorkerGrant(t *testing.T) {
	group, ledger := newGroup(context.Background())
	starter := NewStarter(group, 1)
	release := make(chan struct{})
	started := make(chan struct{})
	if err := starter.Start("first", func(context.Context) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := starter.Start("excess", func(context.Context) error { return nil }); !errors.Is(err, plugin.ErrWorkerLimit) {
		t.Fatalf("excess worker error = %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		starter.mu.Lock()
		active := starter.active
		starter.mu.Unlock()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker grant was not repaid")
		}
		runtime.Gosched()
	}
	if err := starter.Start("next", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if report := group.Wait(context.Background()); !report.Complete() || ledger.Failed() {
		t.Fatalf("repaid report = %#v, ledger = %#v", report, ledger.Events())
	}
}

func TestStarterWithoutWorkerGrantRejectsWork(t *testing.T) {
	if err := NewStarter(New(context.Background(), journal.NewLedger()), 0).Start("work", func(context.Context) error { return nil }); !errors.Is(err, plugin.ErrWorkerLimit) {
		t.Fatalf("zero worker grant error = %v", err)
	}
}
