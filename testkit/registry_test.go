package testkit

import (
	"reflect"
	"strings"
	"testing"
)

func TestCoverageRecordsAssignedUncoveredContracts(t *testing.T) {
	coverage := NewCoverage()
	for _, gap := range []UncoveredContract{
		{Identity: "metadata.mapping-loss", Milestone: "M7"},
		{Identity: "access.direct-inspect", Milestone: "M9"},
		{Identity: "access.snapshot-retry", Milestone: "remote-provider"},
	} {
		if err := coverage.AssignUncovered(gap.Identity, gap.Milestone); err != nil {
			t.Fatal(err)
		}
	}
	want := []UncoveredContract{
		{Identity: "access.direct-inspect", Milestone: "M9"},
		{Identity: "access.snapshot-retry", Milestone: "remote-provider"},
		{Identity: "metadata.mapping-loss", Milestone: "M7"},
	}
	if got := coverage.Uncovered(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Uncovered() = %#v, want %#v", got, want)
	}
	err := coverage.completionError()
	if err == nil || !strings.Contains(err.Error(), "access.direct-inspect (M9)") || !strings.Contains(err.Error(), "access.snapshot-retry (remote-provider)") {
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
