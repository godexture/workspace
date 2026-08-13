package integration_test

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
	"github.com/godexture/godec/testkit"
)

func TestOfficialPluginConformance(t *testing.T) {
	set := standard.Set()
	coverage := testkit.NewCoverage()

	testkit.Plugin(t, file.Plugin())
	testkit.Plugin(t, linear.Plugin())
	testkit.Plugin(t, wave.Plugin())
	runFileCases(t, set, coverage)
	runLinearCases(t, set, coverage)
	runLinearSuggestions(t, set, coverage)
	runWAVECases(t, set, coverage)
	runRIFFInfoCases(t, set, coverage)

	coverage.VerifyExecutable(t, set)
	coverage.VerifyIdentities(t, set, wave.InfoEncodingIdentity())
	for _, assignment := range []struct {
		identity  string
		milestone string
	}{
		{identity: "access.direct-inspect", milestone: "M9"},
		{identity: "metadata.mapping-loss", milestone: "M7"},
		{identity: "endpoint.conformance", milestone: "M9"},
	} {
		if err := coverage.AssignUncovered(assignment.identity, assignment.milestone); err != nil {
			t.Fatal(err)
		}
	}
	wantGaps := []testkit.UncoveredContract{
		{Identity: "access.direct-inspect", Milestone: "M9"},
		{Identity: "endpoint.conformance", Milestone: "M9"},
		{Identity: "metadata.mapping-loss", Milestone: "M7"},
	}
	if got := coverage.Uncovered(); !reflect.DeepEqual(got, wantGaps) {
		t.Fatalf("assigned conformance gaps = %#v, want %#v", got, wantGaps)
	}
}
