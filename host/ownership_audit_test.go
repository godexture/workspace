package host

import (
	"context"
	"testing"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/ownership"
)

func TestVerifyOwnershipAcceptsPersistentOwnerReleasedAtClose(t *testing.T) {
	state := &lifecycleState{retain: true, releaseRetained: true}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request, VerifyOwnership())
	if err != nil || !result.Succeeded() {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestVerifyOwnershipReportsPersistentOwnerLeakedAtClose(t *testing.T) {
	state := &lifecycleState{retain: true}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request, VerifyOwnership())
	if err == nil || result.Primary != nil || len(result.Cleanup) != 1 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	failure := result.Cleanup[0]
	if failure.Phase != ResourcePhase || failure.Node != "sink" || failure.Task != "runtime/ownership" {
		t.Fatalf("ownership failure = %#v", failure)
	}
	imbalance, ok := failure.Err.(*journal.OwnershipError)
	if !ok || imbalance.Live != 1 || imbalance.Overrelease != 0 {
		t.Fatalf("ownership detail = %#v", failure.Err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "host.cleanup.ownership" ||
		result.Diagnostics[0].Detail["live"] != "1" || result.Diagnostics[0].Detail["overrelease"] != "0" {
		t.Fatalf("ownership diagnostics = %#v", result.Diagnostics)
	}
}

func TestVerifyOwnershipBalancesFanoutAndQueuedMoves(t *testing.T) {
	state := &lifecycleState{multi: true}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request, VerifyOwnership())
	if err != nil || !result.Succeeded() {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestOwnershipOverreleaseProjectsAsStructuredCleanup(t *testing.T) {
	ledger := journal.NewLedger()
	ledger.EnableOwnershipAudit()
	reporter := ledger.Domain("task", "node").At("node").Reporter()
	ownership.Track(reporter, -1)
	ledger.RecordOwnershipFailures()
	r := runner{ledger: ledger, diag: &diagnosticLog{}}
	r.collect()
	if r.result.Primary != nil || len(r.result.Cleanup) != 1 {
		t.Fatalf("result = %#v", r.result)
	}
	failure := r.result.Cleanup[0]
	imbalance, ok := failure.Err.(*journal.OwnershipError)
	if !ok || failure.Phase != ResourcePhase || imbalance.Live != -1 || imbalance.Overrelease != 1 {
		t.Fatalf("failure = %#v", failure)
	}
	diagnostics := r.diag.snapshot()
	if len(diagnostics) != 1 || diagnostics[0].Code != "host.cleanup.ownership" ||
		diagnostics[0].Detail["live"] != "-1" || diagnostics[0].Detail["overrelease"] != "1" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
