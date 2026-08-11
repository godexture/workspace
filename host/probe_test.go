package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	internalplanning "github.com/godexture/godec/internal/planning"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type (
	probeFixturePluginID  struct{}
	probeFixtureConfigID  struct{}
	probeContentID        struct{}
	probeFallbackID       struct{}
	probeContentFormatID  struct{}
	probeFallbackFormatID struct{}
	probeFixtureConfig    struct{}
	probeFixturePlan      struct{ shape flow.Shape }
	probeFixtureOperator  struct{ shape flow.Shape }
)

func (o probeFixtureOperator) Ports() flow.Shape { return o.shape.Clone() }
func (probeFixtureOperator) Close() error        { return nil }

type probeTestSession struct {
	data            []byte
	capabilities    access.Capabilities
	cursor          int
	randomReads     atomic.Int32
	sequentialReads atomic.Int32
	positionedBytes atomic.Int64
	sequentialBytes atomic.Int64
	closed          atomic.Int32
}

func (s *probeTestSession) Capabilities() access.Capabilities { return s.capabilities }

func (s *probeTestSession) Read(_ context.Context, destination []byte) (int, error) {
	s.sequentialReads.Add(1)
	if s.cursor >= len(s.data) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[s.cursor:])
	s.cursor += count
	s.sequentialBytes.Add(int64(count))
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *probeTestSession) ReadAt(_ context.Context, destination []byte, offset int64) (int, error) {
	s.randomReads.Add(1)
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	s.positionedBytes.Add(int64(count))
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *probeTestSession) Close() error {
	s.closed.Add(1)
	return nil
}

func TestProbeStoreDeduplicatesSharedRandomRanges(t *testing.T) {
	data := []byte("RIFF\x10\x00\x00\x00WAVEpayload")
	session, opening := probeOpening(t, data, access.RandomRead, access.SequentialRead, access.StableSize)
	store, err := newProbeStore(opening, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := access.NewRangeRequest(0, 12)
	requests := make([]access.RangeRequest, 100)
	for index := range requests {
		requests[index] = request
	}
	progress, _, err := store.Extend(t.Context(), requests, job.DefaultBudget().ProbeBytes)
	if err != nil || !progress {
		t.Fatalf("Extend progress=%v, error=%v", progress, err)
	}
	contained, _ := access.NewRangeRequest(2, 4)
	progress, _, err = store.Extend(t.Context(), []access.RangeRequest{contained}, job.DefaultBudget().ProbeBytes)
	if err != nil || progress {
		t.Fatalf("contained Extend progress=%v, error=%v", progress, err)
	}
	if session.randomReads.Load() != 1 || session.positionedBytes.Load() != 12 || session.sequentialReads.Load() != 0 || session.cursor != 0 {
		t.Fatalf("source reads = random %d/%d bytes, sequential %d, cursor %d", session.randomReads.Load(), session.positionedBytes.Load(), session.sequentialReads.Load(), session.cursor)
	}
	if len(store.Views()) != 1 || store.read != 12 {
		t.Fatalf("cache = %d views, %d bytes", len(store.Views()), store.read)
	}
	retained, err := finishProbeStores([]*probeStore{store})
	if err != nil || len(retained) != 0 {
		t.Fatalf("random cache retained=%d, error=%v", len(retained), err)
	}
}

func TestSequentialProbePrefixMovesIntoSameReplaySession(t *testing.T) {
	data := []byte("0123456789abcdef")
	session, opening := probeOpening(t, data, access.SequentialRead)
	store, err := newProbeStore(opening, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := access.NewRangeRequest(8, 4)
	if progress, _, extendErr := store.Extend(t.Context(), []access.RangeRequest{request}, job.DefaultBudget().ProbeBytes); extendErr != nil || !progress {
		t.Fatalf("Extend progress=%v, error=%v", progress, extendErr)
	}
	if session.cursor != 12 || store.read != 12 {
		t.Fatalf("probe cursor=%d, bytes=%d", session.cursor, store.read)
	}
	replayed, err := store.ReplaySession(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.handles) != 0 || len(store.views) != 0 {
		t.Fatal("prefix ownership was not moved from the probe store")
	}
	reader := replayed.(access.Sequential)
	var output bytes.Buffer
	for _, size := range []int{3, 5, 2, 11} {
		destination := make([]byte, size)
		count, readErr := reader.Read(t.Context(), destination)
		output.Write(destination[:count])
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			t.Fatal(readErr)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	for output.Len() < len(data) {
		destination := make([]byte, 7)
		count, readErr := reader.Read(t.Context(), destination)
		output.Write(destination[:count])
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	if !bytes.Equal(output.Bytes(), data) {
		t.Fatalf("replayed bytes = %q, want %q", output.Bytes(), data)
	}
	if session.sequentialBytes.Load() != int64(len(data)) {
		t.Fatalf("underlying source consumed %d bytes, want %d", session.sequentialBytes.Load(), len(data))
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	if session.closed.Load() != 1 {
		t.Fatalf("underlying session closed %d times", session.closed.Load())
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReplaySessionCloseReleasesUnreadPrefix(t *testing.T) {
	session, opening := probeOpening(t, []byte("0123456789abcdef"), access.SequentialRead)
	store, err := newProbeStore(opening, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := access.NewRangeRequest(0, 12)
	if progress, _, extendErr := store.Extend(t.Context(), []access.RangeRequest{request}, job.DefaultBudget().ProbeBytes); extendErr != nil || !progress {
		t.Fatalf("Extend progress=%v, error=%v", progress, extendErr)
	}
	replayed, err := store.ReplaySession(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if session.closed.Load() != 1 {
		t.Fatalf("underlying session closed %d times", session.closed.Load())
	}
}

func TestProbeSchedulerSelectsContentBeforeFallbackWithinBudget(t *testing.T) {
	instance := probeHost(t)
	data := []byte("RIFF\x10\x00\x00\x00WAVEpayload")
	session, opening := probeOpening(t, data, access.RandomRead, access.SequentialRead, access.StableSize)
	choice, store, usage, err := instance.probeInput(t.Context(), probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if choice.component.Identity() != plugin.IdentityOf[probeContentID]() || choice.fallback || len(choice.evidence) != 1 {
		t.Fatalf("choice = %s, fallback=%v, evidence=%v", choice.component.Identity(), choice.fallback, choice.evidence)
	}
	if usage.ProbeBytes != 12 || usage.ProbeRounds != 2 || session.randomReads.Load() != 1 || session.sequentialReads.Load() != 0 {
		t.Fatalf("usage=%#v, reads=%d/%d", usage, session.randomReads.Load(), session.sequentialReads.Load())
	}
}

func TestProbeSchedulerReportsByteAndRoundExhaustion(t *testing.T) {
	instance := probeHost(t)
	data := []byte("RIFF\x10\x00\x00\x00WAVEpayload")
	tests := map[string]struct {
		budget    job.Budget
		dimension string
	}{
		"bytes":  {budget: probeBudget(11, 4), dimension: "bytes"},
		"rounds": {budget: probeBudget(64, 1), dimension: "rounds"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			session, opening := probeOpening(t, data, access.RandomRead, access.StableSize)
			_, _, _, err := instance.probeInput(t.Context(), probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, test.budget)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != "prepare.probe-budget" || items[0].Detail["dimension"] != test.dimension || !strings.Contains(items[0].Detail["candidate"], plugin.IdentityOf[probeContentID]().String()) {
				t.Fatalf("diagnostic = %#v, error=%v", items, err)
			}
			if test.dimension == "bytes" && items[0].Detail["range"] != "0:12" {
				t.Fatalf("unmet range = %#v", items[0].Detail)
			}
		})
	}
}

func TestProbeSchedulerReportsSharedPlanningDurationExhaustion(t *testing.T) {
	instance := probeHost(t)
	budget := job.DefaultBudget()
	budget.Duration = 1
	ctx, cancel := internalplanning.Start(t.Context(), budget.Duration)
	defer cancel()
	<-ctx.Done()
	session, opening := probeOpening(t, []byte("RIFF\x10\x00\x00\x00WAVE"), access.RandomRead, access.StableSize)
	_, _, _, err := instance.probeInput(ctx, probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, budget)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "prepare.probe-budget" || items[0].Detail["dimension"] != "duration" || items[0].Detail["durationLimit"] != budget.Duration.String() {
		t.Fatalf("diagnostic = %#v, error=%v", items, err)
	}
}

func TestChooseFormatReportsCanonicalEqualRankAmbiguity(t *testing.T) {
	instance := probeHost(t)
	contentComponent, _ := instance.index.Lookup(plugin.IdentityOf[probeContentID]())
	fallbackComponent, _ := instance.index.Lookup(plugin.IdentityOf[probeFallbackID]())
	evidence, _ := mediaformat.NewEvidence("content")
	first := []probeCandidate{
		{component: contentComponent, result: mediaformat.Match(evidence), terminal: true},
		{component: fallbackComponent, result: mediaformat.Match(evidence), terminal: true},
	}
	_, firstErr := chooseFormat(probeBoundary(), first)
	_, secondErr := chooseFormat(probeBoundary(), []probeCandidate{first[1], first[0]})
	firstItems := diagnostic.ItemsOf(firstErr)
	secondItems := diagnostic.ItemsOf(secondErr)
	if len(firstItems) != 1 || len(secondItems) != 1 || firstItems[0].Code != "prepare.probe-ambiguous" || firstItems[0].Detail["candidates"] != secondItems[0].Detail["candidates"] || firstItems[0].Detail["evidence"] != secondItems[0].Detail["evidence"] {
		t.Fatalf("ambiguity diagnostics = %#v / %#v", firstItems, secondItems)
	}
}

func probeOpening(t *testing.T, data []byte, capabilities ...access.Capability) (*probeTestSession, access.Opening) {
	t.Helper()
	available, err := access.NewCapabilities(capabilities...)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := probeSelectionForTest(available)
	if !ok {
		t.Fatal("no readable selection")
	}
	session := &probeTestSession{data: append([]byte(nil), data...), capabilities: available}
	opening, err := access.NewOpening(access.SourceDirection, session, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	return session, opening
}

func probeSelectionForTest(capabilities access.Capabilities) (access.Selection, bool) {
	for _, capability := range []access.Capability{access.RandomRead, access.SequentialRead} {
		if selection, ok := access.Select(capabilities, access.NewRequirements(access.AnyOf(capability))); ok {
			return selection, true
		}
	}
	return access.Selection{}, false
}

func probeHost(t *testing.T) *Host {
	t.Helper()
	content := probeFormatComponent[probeContentID, probeContentFormatID](probeContent)
	fallback := probeFormatComponent[probeFallbackID, probeFallbackFormatID](func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
		return mediaformat.Fallback(), nil
	})
	set := plugin.NewSet(plugin.Define[probeFixturePluginID](plugin.Descriptor{DisplayName: "probe fixture", Version: "1"}, content, fallback))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func probeFormatComponent[ComponentMarker, FormatMarker any](probe mediaformat.ProbeFunc) plugin.Component {
	configuration := config.Struct[probeFixtureConfigID](func() probeFixtureConfig { return probeFixtureConfig{} }).Version("1").Build()
	shape := flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("chunks", mediaformat.Chunks())},
	)
	spec := plugin.Spec[probeFixtureConfig, probeFixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[probeFixtureConfig](shape),
		Compile: func(plugin.CompileContext, probeFixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[probeFixturePlan, stream.Descriptor], error) {
			return plugin.Compiled[probeFixturePlan, stream.Descriptor]{Plan: probeFixturePlan{shape: shape.Clone()}}, nil
		},
		Open: func(plugin.OpenContext, probeFixturePlan) (flow.Operator, error) {
			return probeFixtureOperator{shape: shape.Clone()}, nil
		},
	}
	format, err := mediaformat.Define[FormatMarker](nil)
	if err != nil {
		panic(err)
	}
	return plugin.NewComponent[ComponentMarker](
		plugin.Descriptor{DisplayName: "probe Format fixture"},
		configuration,
		plugin.WithSpec(spec),
		mediaformat.Read(format, access.NewRequirements(access.AnyOf(access.RandomRead)), mediaformat.WithProbe(probe)),
	)
}

func probeContent(ctx mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
	request, _ := access.NewRangeRequest(0, 12)
	for _, view := range ctx.Views() {
		if view.Base() != 0 || view.Size() < 12 {
			continue
		}
		value := view.Bytes()
		if string(value[:4]) != "RIFF" || string(value[8:12]) != "WAVE" {
			return mediaformat.Mismatch(), nil
		}
		evidence, _ := mediaformat.NewEvidence("RIFF/WAVE signature")
		return mediaformat.Match(evidence), nil
	}
	return mediaformat.Need(request), nil
}

func probeBoundary() plan.Boundary {
	return plan.Boundary{Node: "source", Scheme: "memory", Direction: plan.InputBoundary}
}

func probeBudget(byteLimit resource.Bytes, rounds int) job.Budget {
	value := job.DefaultBudget()
	value.ProbeBytes = byteLimit
	value.ProbeRounds = rounds
	return value
}

var _ access.Session = (*probeTestSession)(nil)
var _ access.Sequential = (*probeTestSession)(nil)
var _ access.Random = (*probeTestSession)(nil)
