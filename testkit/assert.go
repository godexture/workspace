// What the runner checks once a scenario has run: the Plan selected the
// subject, and every fixture operator was opened and closed exactly once.
package testkit

import (
	"errors"
	"fmt"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (f failureExpectation) matches(err error) bool {
	if f.target != nil {
		return errors.Is(err, f.target)
	}
	return f.code != "" && hasDiagnostic(err, f.code)
}

func (f failureExpectation) describe() string {
	if f.target != nil {
		return fmt.Sprintf("error matching %v", f.target)
	}
	return fmt.Sprintf("diagnostic %q", f.code)
}

func hasDiagnostic(err error, code string) bool {
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == code {
			return true
		}
	}
	return false
}

// assertNoIncidentalFailures fixes what a run must not produce beside the one
// failure a scenario is about.
//
// Secondary and Cleanup are separate axes, and a component that leaks a
// release or fails a second time independently shows up in exactly one of
// them. Checking only Cleanup would let an independent second failure pass
// unnoticed now that the Result can express one.
func assertNoIncidentalFailures(t testing.TB, scenario string, result host.Result) {
	t.Helper()
	if len(result.Cleanup) != 0 {
		t.Errorf("%s cleanup failures = %v", scenario, result.Cleanup)
	}
	if len(result.Secondary) != 0 {
		t.Errorf("%s independent failures beside the expected one = %v", scenario, result.Secondary)
	}
	// A repeated failure is summarised rather than copied, so a component that
	// leaks releases in bulk shows up here and nowhere else.
	for _, suppressed := range result.Suppressed {
		t.Errorf("%s repeated failure: %v", scenario, suppressed)
	}
}

func assertSelectedSubject(t testing.TB, selected plan.Plan, identity plugin.Identity) {
	t.Helper()
	count := 0
	for _, node := range selected.Nodes() {
		if node.Component != identity.String() {
			continue
		}
		count++
		if node.Origin != plan.Requested {
			t.Errorf("testkit subject %s origin = %v, want requested", identity, node.Origin)
		}
	}
	if count != 1 {
		t.Fatalf("testkit selected subject %s %d times, want once", identity, count)
	}
}

func assertLifecycle(t testing.TB, state *lifecycleState, requireEOF bool) {
	t.Helper()
	if state == nil {
		t.Errorf("testkit fixture lifecycle state is absent")
		return
	}
	if state.sourceOpen.Load() != 1 || state.sourceClose.Load() != 1 || state.sinkOpen.Load() != 1 || state.sinkClose.Load() != 1 {
		t.Errorf("testkit fixture lifecycle source(open=%d close=%d) sink(open=%d close=%d), want one each",
			state.sourceOpen.Load(), state.sourceClose.Load(), state.sinkOpen.Load(), state.sinkClose.Load())
	}
	if requireEOF && state.eof.Load() == 0 {
		t.Errorf("testkit fixture source did not reach EOF")
	}
}
