package task

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestStarterEnforcesAndRepaysWorkerGrant(t *testing.T) {
	group := New(context.Background())
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
	if err := starter.Start("excess", func(context.Context) error { return nil }); !errors.Is(err, ErrWorkerLimit) {
		t.Fatalf("excess worker error = %v", err)
	}
	close(release)
	for {
		starter.mu.Lock()
		active := starter.active
		starter.mu.Unlock()
		if active == 0 {
			break
		}
		runtime.Gosched()
	}
	if err := starter.Start("next", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if report := group.Wait(context.Background()); !report.Complete() || len(report.Failures) != 0 {
		t.Fatalf("repaid report = %#v", report)
	}
}

func TestStarterWithoutWorkerGrantRejectsWork(t *testing.T) {
	if err := NewStarter(New(context.Background()), 0).Start("work", func(context.Context) error { return nil }); !errors.Is(err, ErrWorkerLimit) {
		t.Fatalf("zero worker grant error = %v", err)
	}
}
