package testkit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/plugin"
)

func TestCoverageRecordsAssignedUncoveredContracts(t *testing.T) {
	coverage := NewCoverage()
	for _, gap := range []UncoveredContract{
		{Identity: "metadata.mapping-loss", Milestone: "M7"},
		{Identity: "access.direct-inspect", Milestone: "M9"},
		{Identity: "endpoint.conformance", Milestone: "M9"},
	} {
		if err := coverage.AssignUncovered(gap.Identity, gap.Milestone); err != nil {
			t.Fatal(err)
		}
	}
	want := []UncoveredContract{
		{Identity: "access.direct-inspect", Milestone: "M9"},
		{Identity: "endpoint.conformance", Milestone: "M9"},
		{Identity: "metadata.mapping-loss", Milestone: "M7"},
	}
	if got := coverage.Uncovered(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Uncovered() = %#v, want %#v", got, want)
	}
	err := coverage.completionError()
	if err == nil || !strings.Contains(err.Error(), "access.direct-inspect (M9)") || !strings.Contains(err.Error(), "endpoint.conformance (M9)") {
		t.Fatalf("completion error = %v", err)
	}
	if err := coverage.AssignUncovered("access.direct-inspect", "M10"); err == nil || !strings.Contains(err.Error(), "already assigned to M9") {
		t.Fatalf("duplicate assignment error = %v", err)
	}
}

func TestCoverageRejectsUnownedGaps(t *testing.T) {
	tests := []struct {
		name      string
		coverage  *Coverage
		identity  string
		milestone string
		message   string
	}{
		{name: "nil registry", identity: "endpoint.runtime", milestone: "M9", message: "nil registry"},
		{name: "empty identity", coverage: NewCoverage(), milestone: "M9", message: "identity is required"},
		{name: "empty milestone", coverage: NewCoverage(), identity: "endpoint.runtime", message: "no responsible milestone"},
		{name: "off-roadmap milestone", coverage: NewCoverage(), identity: "endpoint.runtime", milestone: "remote-provider", message: "is not one of"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.coverage.AssignUncovered(test.identity, test.milestone)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("AssignUncovered() error = %v", err)
			}
		})
	}
}

func TestEmptyCoverageIsComplete(t *testing.T) {
	if err := NewCoverage().completionError(); err != nil {
		t.Fatal(err)
	}
	var coverage *Coverage
	if err := coverage.completionError(); err == nil || !strings.Contains(err.Error(), "registry is nil") {
		t.Fatalf("nil completion error = %v", err)
	}
}

func TestCoverageRejectsExecutableWithoutExecutedTypedCase(t *testing.T) {
	problems := NewCoverage().executableProblems(plugin.NewSet(runnerDefinition()))
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), plugin.IdentityOf[runnerComponentID]().String()) || !strings.Contains(problems[0].Error(), "no executed typed case") {
		t.Fatalf("missing executable coverage problems = %v", problems)
	}
}

// Coverage claims every recorded identity belongs to the Set it verifies
// against. A Suggest scenario recorded for a component outside that Set is as
// wrong as an executed case would be.
func TestCoverageRejectsForeignSuggestIdentity(t *testing.T) {
	set := plugin.NewSet(runnerDefinition())
	coverage := NewCoverage()
	coverage.record(plugin.IdentityOf[runnerComponentID]())
	coverage.recordSuggest(plugin.IdentityOf[runnerComponentID]())
	coverage.recordSuggest(plugin.IdentityOf[coverageForeignID]())

	problems := coverage.executableProblems(set)
	found := false
	for _, problem := range problems {
		if strings.Contains(problem.Error(), "suggested identity") && strings.Contains(problem.Error(), "coverageForeignID") {
			found = true
		}
	}
	if !found {
		t.Fatalf("foreign Suggest identity was accepted: %v", problems)
	}
}

type coverageForeignID struct{}
