package integration_test

import (
	"reflect"
	"testing"

	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/mp4"
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
	testkit.Plugin(t, mp4.Plugin())
	runFileCases(t, set, coverage)
	runLinearCases(t, set, coverage)
	runLinearSuggestions(t, set, coverage)
	runWAVECases(t, set, coverage)
	runRIFFInfoCases(t, set, coverage)

	// The shared typed runner models one stream and cannot provide MP4's
	// inspected-source handoff to the same-format muxer. MP4's explicit
	// multi-track Host E2E is the semantic execution gate; keep this registry
	// scoped to the families represented by the common runner below.
	testkitSet := plugin.NewSet(file.Plugin(), linear.Plugin(), wave.Plugin())
	coverage.VerifyExecutable(t, testkitSet)
	coverage.VerifyIdentities(t, testkitSet, wave.InfoEncodingIdentity())
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
