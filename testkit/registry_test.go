package testkit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
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
	if got := coverage.Uncovered(); len(got) != len(want) {
		t.Fatalf("Uncovered() = %#v, want %d assigned gaps", got, len(want))
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

// Observe takes its evidence from an executed Plan, so it must reject a Plan it
// cannot attribute: nothing ran, or a node belongs to another composition.
func TestCoverageObserveRejectsUnattributablePlans(t *testing.T) {
	known := plugin.IdentityOf[runnerComponentID]().String()
	set := plugin.NewSet(runnerDefinition())

	if err := (*Coverage)(nil).observe(observedPlan(t, known), set); err == nil || !strings.Contains(err.Error(), "registry is nil") {
		t.Fatalf("nil registry error = %v", err)
	}
	if err := NewCoverage().observe(plan.Plan{}, set); err == nil || !strings.Contains(err.Error(), "Plan is invalid") {
		t.Fatalf("invalid Plan error = %v", err)
	}
	foreign := NewCoverage()
	if err := foreign.observe(observedPlan(t, "acme.transform"), set); err == nil || !strings.Contains(err.Error(), "outside the composition") {
		t.Fatalf("foreign node error = %v", err)
	}
	if len(foreign.executed) != 0 {
		t.Fatalf("rejected Plan recorded coverage: %#v", foreign.executed)
	}

	coverage := NewCoverage()
	if err := coverage.observe(observedPlan(t, known), set); err != nil {
		t.Fatal(err)
	}
	if coverage.executed[plugin.IdentityOf[runnerComponentID]()] != 1 {
		t.Fatalf("observed executions = %#v", coverage.executed)
	}
}

func observedPlan(t *testing.T, component string) plan.Plan {
	t.Helper()
	subject, ok := componentOf(plugin.NewSet(runnerDefinition()), plugin.IdentityOf[runnerComponentID]())
	if !ok {
		t.Fatal("runner component is absent from its own definition")
	}
	resolved, err := subject.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := job.PolicyFor(job.Fast)
	contract := plugin.DefaultContract()
	value, err := plan.New(plan.Description{
		RequestedPolicy:    policy,
		EffectivePolicy:    policy,
		Budget:             job.DefaultBudget(),
		CatalogFingerprint: "catalog",
		Platform:           plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"},
		Nodes: []plan.Node{{
			ID: "only", Origin: plan.Requested, Component: component, DisplayName: "Only",
			Variant: component + "#default", Version: "1", Config: resolved.Summary(), Contract: contract,
		}},
		Scratch: plan.Scratch{Limit: policy.Resources.ScratchMaxBytes, TemporaryLimit: policy.Resources.TemporaryMaxBytes},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
