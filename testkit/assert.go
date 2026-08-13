// What the runner checks once a scenario has run: the Plan selected the
// subject, and every fixture operator was opened and closed exactly once.
package testkit

import (
	"errors"
	"fmt"
	"testing"

	"github.com/godexture/godec/diagnostic"
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
