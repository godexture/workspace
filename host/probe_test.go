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
	probeOtherFallbackID  struct{}
	probeContentFormatID  struct{}
	probeFallbackFormatID struct{}
	probeOtherFormatID    struct{}
	probeFixtureConfig    struct{ Rate int }
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
	fallback, _ := mediaformat.Define[probeFallbackFormatID](nil)
	hint, _ := job.SelectFormat(fallback)
	choice, store, usage, err := instance.probeInput(t.Context(), probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, hint, job.DefaultBudget())
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
			_, _, _, err := instance.probeInput(t.Context(), probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, job.FormatSelector{}, test.budget)
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
	_, _, _, err := instance.probeInput(ctx, probeBoundary(), acquiredSession{node: "source", value: session, actual: session.capabilities, opening: opening}, job.FormatSelector{}, budget)
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
	_, firstErr := instance.chooseFormat(probeBoundary(), first, job.FormatSelector{})
	_, secondErr := instance.chooseFormat(probeBoundary(), []probeCandidate{first[1], first[0]}, job.FormatSelector{})
	firstItems := diagnostic.ItemsOf(firstErr)
	secondItems := diagnostic.ItemsOf(secondErr)
	if len(firstItems) != 1 || len(secondItems) != 1 || firstItems[0].Code != "prepare.probe-ambiguous" || firstItems[0].Detail["candidates"] != secondItems[0].Detail["candidates"] || firstItems[0].Detail["evidence"] != secondItems[0].Detail["evidence"] {
		t.Fatalf("ambiguity diagnostics = %#v / %#v", firstItems, secondItems)
	}
}

func TestFallbackRequiresExplicitHintAndRequiredConfig(t *testing.T) {
	instance := probeHost(t)
	component, _ := instance.index.Lookup(plugin.IdentityOf[probeFallbackID]())
	trait, _ := mediaformat.ReadOf(component)
	candidates := []probeCandidate{{component: component, trait: trait, result: mediaformat.Fallback(), terminal: true}}

	if _, err := instance.chooseFormat(probeBoundary(), candidates, job.FormatSelector{}); diagnostic.ItemsOf(err)[0].Code != "prepare.probe-mismatch" {
		t.Fatalf("fallback without hint = %v", err)
	}
	hint, _ := job.SelectFormat(trait.Format())
	if _, err := instance.chooseFormat(probeBoundary(), candidates, hint); diagnostic.ItemsOf(err)[0].Code != "prepare.format-config-required" {
		t.Fatalf("fallback without config = %v", err)
	}
	rate, hasRate := component.Schema().Key("rate")
	if !hasRate {
		t.Fatal("fallback component has no rate field key")
	}
	hint = hint.WithConfig(config.NewPatch().Set(rate, 1))
	choice, err := instance.chooseFormat(probeBoundary(), candidates, hint)
	if err != nil {
		t.Fatal(err)
	}
	patch, configured := choice.config, choice.configured
	if !choice.fallback || !configured || len(patch.FieldIDs()) != 1 || patch.FieldIDs()[0] != "rate" {
		t.Fatalf("configured fallback = %#v, fields %v", choice, patch.FieldIDs())
	}
}

func TestFallbackExtensionAmbiguityIsResolvedThroughCatalog(t *testing.T) {
	first := probeFormatComponentWithExtensions[probeFallbackID, probeFallbackFormatID](func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
		return mediaformat.Fallback(), nil
	}, []string{"shared"})
	second := probeFormatComponentWithExtensions[probeOtherFallbackID, probeOtherFormatID](func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
		return mediaformat.Fallback(), nil
	}, []string{"shared"})
	instance, err := New(Plugins(plugin.NewSet(plugin.Define[probeFixturePluginID](plugin.Descriptor{DisplayName: "ambiguous", Version: "1"}, first, second))))
	if err != nil {
		t.Fatal(err)
	}
	candidates := make([]probeCandidate, 0, 2)
	for _, component := range []plugin.Component{first, second} {
		trait, _ := mediaformat.ReadOf(component)
		candidates = append(candidates, probeCandidate{component: component, trait: trait, result: mediaformat.Fallback(), terminal: true})
	}
	extension, _ := mediaformat.ParseExtension("shared")
	hint, _ := job.SelectFormatExtension(extension)
	_, err = instance.chooseFormat(probeBoundary(), candidates, hint)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "prepare.format-ambiguous" || !strings.Contains(items[0].Detail["candidates"], first.Identity().String()) || !strings.Contains(items[0].Detail["candidates"], second.Identity().String()) {
		t.Fatalf("extension ambiguity = %#v", items)
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
		if selection, ok := access.Select(capabilities, access.NewRequirements(access.AllOf(capability))); ok {
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
	}, mediaformat.RequireFallbackConfig("rate"))
	set := plugin.NewSet(plugin.Define[probeFixturePluginID](plugin.Descriptor{DisplayName: "probe fixture", Version: "1"}, content, fallback))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func probeFormatComponent[ComponentMarker, FormatMarker any](probe mediaformat.ProbeFunc, options ...mediaformat.ReadOption) plugin.Component {
	return probeFormatComponentWithExtensions[ComponentMarker, FormatMarker](probe, nil, options...)
}

func probeFormatComponentWithExtensions[ComponentMarker, FormatMarker any](probe mediaformat.ProbeFunc, extensions []string, options ...mediaformat.ReadOption) plugin.Component {
	configuration := config.Struct[probeFixtureConfigID](func() probeFixtureConfig { return probeFixtureConfig{Rate: 1} }).Version("1").
		AddField(config.Field("rate", func(value *probeFixtureConfig) *int { return &value.Rate }, config.Int().Range(1, 768_000))).Build()
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
	var formatOptions []mediaformat.Option
	if len(extensions) != 0 {
		formatOptions = append(formatOptions, mediaformat.WithExtensions(extensions...))
	}
	format, err := mediaformat.Define[FormatMarker](nil, formatOptions...)
	if err != nil {
		panic(err)
	}
	return plugin.NewComponent[ComponentMarker](
		plugin.Descriptor{DisplayName: "probe Format fixture"},
		configuration,
		plugin.WithSpec(spec),
		mediaformat.Read(format, access.NewRequirements(access.AllOf(access.RandomRead)), append([]mediaformat.ReadOption{mediaformat.WithProbe(probe)}, options...)...),
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

type snapshotProbeSession struct {
	*probeTestSession
	identity string
}

func (s *snapshotProbeSession) Snapshot(context.Context) (access.Snapshot, error) {
	return access.NewSnapshot(s.identity, access.WeakSnapshot)
}

// Replacing a sequential-only session with its replay wrapper must not change
// what the source says about its content. A wrapper that answered for itself
// would report no identity, and the run-time check would read that as the
// source having changed under a job that never touched it.
func TestReplaySessionKeepsTheContentIdentityOfItsSource(t *testing.T) {
	session, opening := probeOpening(t, []byte("0123456789abcdef"), access.SequentialRead)
	snapshotting := &snapshotProbeSession{probeTestSession: session, identity: "fixture/size:16"}
	store, err := newProbeStore(opening, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := access.NewRangeRequest(0, 4)
	if _, _, extendErr := store.Extend(t.Context(), []access.RangeRequest{request}, job.DefaultBudget().ProbeBytes); extendErr != nil {
		t.Fatal(extendErr)
	}
	replayed, err := store.ReplaySession(snapshotting)
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()

	before, err := readSnapshot(t.Context(), snapshotting)
	if err != nil || !before.Valid() {
		t.Fatalf("source snapshot = %#v, error %v", before, err)
	}
	after, err := readSnapshot(t.Context(), replayed)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("replayed snapshot = %#v, want the source snapshot %#v", after, before)
	}
	if failure := verifySnapshots(t.Context(), RunPhase, []acquiredSession{{node: "source", value: replayed, snapshot: before}}); failure != nil {
		t.Fatalf("an unchanged source was reported as changed: %v", failure)
	}
}

// A session with no content identity is not the same as one that changed, and
// a wrapper over it must not invent one.
func TestReplaySessionReportsNoIdentityWhenItsSourceHasNone(t *testing.T) {
	session, opening := probeOpening(t, []byte("0123456789abcdef"), access.SequentialRead)
	store, err := newProbeStore(opening, job.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := access.NewRangeRequest(0, 4)
	if _, _, extendErr := store.Extend(t.Context(), []access.RangeRequest{request}, job.DefaultBudget().ProbeBytes); extendErr != nil {
		t.Fatal(extendErr)
	}
	replayed, err := store.ReplaySession(session)
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()
	snapshot, err := readSnapshot(t.Context(), replayed)
	if err != nil || snapshot.Valid() {
		t.Fatalf("snapshot = %#v, error %v, want no identity and no error", snapshot, err)
	}
}
