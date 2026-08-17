// The public typed-case runner: it turns each case into a scenario, drives
// planning, cancellation, and execution, and reports what did not hold.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

const activeCancellationTimeout = 5 * time.Second

type runnerKind uint8

const (
	componentRunner runnerKind = iota + 1
	formatRunner
	codecRunner
)

func runCases[I, O any](t testing.TB, kind runnerKind, subject Subject[I, O], cases []Case[I, O]) {
	t.Helper()
	if !subject.valid() {
		t.Fatalf("testkit typed case subject is invalid")
	}
	if len(cases) == 0 {
		t.Fatalf("testkit typed case requires at least one case")
	}
	for index := range cases {
		current := cases[index]
		name := current.Name
		if name == "" {
			name = fmt.Sprintf("case-%d", index+1)
		}
		runNamed(t, name, func(child testing.TB) {
			runOne(child, kind, subject, current)
		})
	}
}

func runNamed(t testing.TB, name string, run func(testing.TB)) {
	t.Helper()
	if concrete, ok := t.(*testing.T); ok {
		concrete.Run(name, func(child *testing.T) { run(child) })
		return
	}
	run(t)
}

func runOne[I, O any](t testing.TB, kind runnerKind, subject Subject[I, O], test Case[I, O]) {
	t.Helper()
	if !test.Input.valid() {
		t.Fatalf("testkit typed case input fixture is invalid")
	}
	if !test.Want.valid() {
		t.Fatalf("testkit typed case expectation is invalid")
	}
	master := test.Input
	defer func() {
		if err := master.close(); err != nil {
			t.Errorf("testkit input ownership: %v", err)
		}
	}()
	if kind == formatRunner {
		if err := verifyFormatProbe(subject, master, test.Want.failure.stage != 0); err != nil {
			t.Fatalf("testkit Format probe: %v", err)
		}
	}

	factory := func() (*scenarioCore, error) {
		return newScenario(kind, subject, test.Config, master.clone(), test.Want.newRecorder())
	}
	executed := executeCase(t, subject.identity, test.Want.failure, factory)
	if executed && test.Want.failure.stage == 0 && acceptsRejection(kind, subject) {
		runRejected(t, kind, subject, test, master.clone())
	}
	if executed {
		subject.coverage.record(subject.identity)
	}
}

// executeCase returns whether a real Prepared.Run scenario was exercised.
// Planning-only failures intentionally stop after Host.Plan and do not count
// toward executable runtime coverage.
func executeCase(t testing.TB, identity plugin.Identity, failure failureExpectation, factory func() (*scenarioCore, error)) bool {
	t.Helper()
	if failure.stage == planFailure {
		first := planFailureScenario(t, factory, time.Hour, failure)
		second := planFailureScenario(t, factory, 2*time.Hour, failure)
		if first != second {
			t.Fatalf("planning failure purity: result changed with deadline: %q != %q", first, second)
		}
		return false
	}
	first := planScenario(t, identity, factory, time.Hour)
	second := planScenario(t, identity, factory, 2*time.Hour)
	if first.fingerprint != second.fingerprint {
		t.Fatalf("Compile purity: planning result changed with deadline: %s != %s", first.fingerprint, second.fingerprint)
	}
	runCancelled(t, factory, failure.stage == 0)
	runSuccessful(t, failure, factory, first.plan)
	return true
}

func planFailureScenario(t testing.TB, factory func() (*scenarioCore, error), timeout time.Duration, want failureExpectation) string {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit Plan failure scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit Plan failure scenario cleanup: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, planErr := scenario.host.Plan(ctx, scenario.job)
	if planErr == nil {
		t.Fatalf("testkit Host.Plan succeeded, want %s", want.describe())
	}
	if !want.matches(planErr) {
		t.Fatalf("testkit Host.Plan error = %v, want %s", planErr, want.describe())
	}
	return planErr.Error()
}

type scenarioPlan struct {
	plan        plan.Plan
	fingerprint string
}

func planScenario(t testing.TB, identity plugin.Identity, factory func() (*scenarioCore, error), timeout time.Duration) scenarioPlan {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit Plan scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit Plan scenario cleanup: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	selected, err := scenario.host.Plan(ctx, scenario.job)
	if err != nil {
		t.Fatalf("testkit Host.Plan: %v", err)
	}
	selectedIdentity := identity
	if !scenario.selected.IsZero() {
		selectedIdentity = scenario.selected
	}
	assertSelectedSubject(t, selected, selectedIdentity)
	if scenario.inspectPlan != nil {
		if err := scenario.inspectPlan(selected); err != nil {
			t.Errorf("testkit Plan inspection: %v", err)
		}
	}
	fingerprint := selected.Fingerprint().String()
	if scenario.purity != nil {
		value, purityErr := scenario.purity(ctx)
		if purityErr != nil {
			t.Fatalf("testkit planning purity: %v", purityErr)
		}
		fingerprint += ":" + value
	}
	return scenarioPlan{plan: selected, fingerprint: fingerprint}
}

func runCancelled(t testing.TB, factory func() (*scenarioCore, error), active bool) {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit cancellation scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit cancellation cleanup: %v", err)
		}
	}()
	prepared, err := scenario.host.Prepare(context.Background(), scenario.job)
	if err != nil {
		t.Fatalf("testkit cancellation Prepare: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if active && scenario.active != nil {
		scenario.active.enable()
	}
	var result host.Result
	var runErr error
	done := make(chan struct{})
	if !active {
		cancel()
	}
	go func() {
		result, runErr = prepared.Run(ctx, host.VerifyOwnership())
		close(done)
	}()
	if active && scenario.active != nil {
		if waitErr := scenario.active.wait(activeCancellationTimeout); waitErr != nil {
			t.Errorf("testkit cancellation active-run gate: %v", waitErr)
		}
	}
	if active {
		cancel()
	}
	timer := time.NewTimer(activeCancellationTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		if active && scenario.active != nil {
			scenario.active.unblock()
		}
		t.Fatalf("testkit cancellation Run did not terminate after cancellation")
	}
	if active && scenario.active != nil {
		scenario.active.unblock()
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("testkit cancellation Run error = %v, want context.Canceled", runErr)
	}
	assertNoIncidentalFailures(t, "testkit cancellation", result)
	if err := prepared.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("testkit cancellation Prepared.Close: %v", err)
	}
	if scenario.cancelCheck != nil {
		if err := scenario.cancelCheck(); err != nil {
			t.Errorf("testkit cancellation contract: %v", err)
		}
	}
	if active && scenario.state != nil && !scenario.state.cancelObserved.Load() {
		t.Errorf("testkit cancellation callback did not observe context cancellation")
	}
}

func runSuccessful(t testing.TB, failure failureExpectation, factory func() (*scenarioCore, error), planned plan.Plan) {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit execution scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit execution cleanup: %v", err)
		}
	}()
	prepared, err := scenario.host.Prepare(context.Background(), scenario.job)
	if err != nil {
		t.Fatalf("testkit Host.Prepare: %v", err)
	}
	if prepared.Plan().Fingerprint() != planned.Fingerprint() {
		t.Fatalf("Prepare selected %s, Plan selected %s", prepared.Plan().Fingerprint(), planned.Fingerprint())
	}
	if scenario.inspectPlan != nil {
		if err := scenario.inspectPlan(prepared.Plan()); err != nil {
			t.Errorf("testkit prepared Plan inspection: %v", err)
		}
	}
	result, runErr := prepared.Run(context.Background(), host.VerifyOwnership())
	closeErr := prepared.Close()
	if failure.stage == 0 {
		if runErr != nil || closeErr != nil || !result.Succeeded() {
			t.Fatalf("testkit Host.Run failed: run=%v close=%v result=%#v", runErr, closeErr, result)
		}
		if scenario.finish != nil {
			if err := scenario.finish(); err != nil {
				t.Errorf("testkit output: %v", err)
			}
		}
	} else {
		if runErr == nil {
			t.Errorf("testkit Host.Run succeeded, want %s", failure.describe())
		} else if !failure.matches(runErr) {
			t.Errorf("testkit Host.Run error = %v, diagnostics = %v, want %s", runErr, host.Diagnostics(runErr), failure.describe())
		}
		assertNoIncidentalFailures(t, "testkit expected-failure", result)
	}
	assertLifecycle(t, scenario.state, failure.stage == 0)
}
