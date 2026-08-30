package integration_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/file"
	"github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/vorbiscomment"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
	"github.com/godexture/godec/testkit"
)

func TestOfficialPluginConformance(t *testing.T) {
	set := standard.Set()
	coverage := testkit.NewCoverage()

	testkit.Plugin(t, file.Plugin())
	testkit.Plugin(t, id3.Plugin())
	testkit.Plugin(t, vorbiscomment.Plugin())
	testkit.Plugin(t, linear.Plugin())
	testkit.Plugin(t, wave.Plugin())
	testkit.Plugin(t, mp4.Plugin())
	runFileCases(t, set, coverage)
	runLinearCases(t, set, coverage)
	runLinearSuggestions(t, set, coverage)
	runWideCodingCases(t, set, coverage)
	runConversionCases(t, set, coverage)
	runFilterCases(t, set, coverage)
	runCompandedCases(t, set, coverage)
	runADPCMCases(t, set, coverage)
	runCompandedSuggestions(t, set, coverage)
	runADPCMCoderCases(t, set, coverage)
	runWAVECases(t, set, coverage)
	runRIFFInfoCases(t, set, coverage)
	runID3V1Cases(t, set, coverage)
	runID3V2Cases(t, set, coverage)
	runVorbisCommentCases(t, set, coverage)

	// The shared typed runner models one stream and cannot drive MP4's
	// carrier-less reader or the repeated descriptors its muxer consumes. The
	// multi-track Host run is the semantic gate for those, so its Plan is the
	// coverage evidence rather than a case the runner cannot express.
	coverage.Observe(t, mp4ConformancePlan(t), set)
	// The typed runner drives one stream through one input port, so it cannot
	// express a mixer either. Its two-input Host run is the semantic gate, and
	// the Plan it ran is the coverage evidence.
	coverage.Observe(t, mixerConformancePlan(t), set)
	coverage.Observe(t, convolverConformancePlan(t), set)
	coverage.VerifyExecutable(t, set)
	coverage.VerifyIdentities(t, set, wave.InfoEncodingIdentity(), id3.V1EncodingIdentity(), id3.V2EncodingIdentity(), vorbiscomment.EncodingIdentity())
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

// mp4ConformancePlan runs the multi-track MP4 remux through the official Host
// and returns the Plan it executed. Every node in that Plan is a component the
// composition actually drove end to end.
func mp4ConformancePlan(t *testing.T) plan.Plan {
	t.Helper()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "conformance.mp4")
	outputPath := filepath.Join(directory, "conformance-out.mp4")
	if err := os.WriteFile(inputPath, mp4TwoTrackFixture(), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("MP4 conformance Run = %#v, %v", result, runErr)
	}
	executed := prepared.Plan()
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	// The Plan is only coverage evidence while it still contains the components
	// the typed runner cannot reach.
	for _, identity := range []plugin.Identity{mp4.DemuxerIdentity(), mp4.MuxerIdentity()} {
		found := false
		for _, node := range executed.Nodes() {
			found = found || node.Component == identity.String()
		}
		if !found {
			t.Fatalf("MP4 conformance Plan no longer runs %s", identity)
		}
	}
	return executed
}
