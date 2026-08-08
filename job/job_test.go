package job

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type jobSourceID struct{}
type jobSinkID struct{}

type jobDirectHandle struct{ closed atomic.Int32 }

func (h *jobDirectHandle) Close() error {
	h.closed.Add(1)
	return nil
}

func TestChoicesAreExclusiveAndReferencesStayRedacted(t *testing.T) {
	reference, err := access.Parse("https://user:secret@example.com/in?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	input, err := InputFromReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := input.Reference()
	if !ok {
		t.Fatal("reference input lost its reference")
	}
	if _, ok := input.Direct(); ok {
		t.Fatal("reference input also exposed a source")
	}
	if got.String() == reference.Canonical() {
		t.Fatal("ordinary reference rendering exposed canonical secret")
	}
}

func TestDirectChoiceCarriesTypedResourceAndExplicitAdaptor(t *testing.T) {
	handle := &jobDirectHandle{}
	adaptor, err := NewAdaptor(plugin.IdentityOf[jobSourceID](), config.NewPatch().Set("mode", "test"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := InputFromSource(access.Own(handle), adaptor)
	if err != nil {
		t.Fatal(err)
	}
	direct, ok := input.Direct()
	if !ok || direct.Adaptor().Component() != plugin.IdentityOf[jobSourceID]() || direct.Ownership() != access.Owned {
		t.Fatalf("direct choice = %#v", direct)
	}
	opening, ok := direct.Opening().(access.Direct[*jobDirectHandle])
	if !ok || opening.Value() != handle || opening.Ownership() != access.Owned {
		t.Fatalf("typed opening = %#v", direct.Opening())
	}
	if err := direct.Close(); err != nil {
		t.Fatal(err)
	}
	if err := direct.Close(); err != nil || handle.closed.Load() != 1 {
		t.Fatalf("direct close = %v, count = %d", err, handle.closed.Load())
	}
}

func TestGraphRejectsDuplicateIdentityAndMapping(t *testing.T) {
	source := NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch())
	sink := NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch())
	mapping := MapStream(0, stream.ID("audio"), 0)
	_, err := NewGraph(
		[]Node{source, source, sink},
		[]Edge{Connect(At("source", "out"), At("sink", "in"))},
		mapping,
		mapping,
	)
	if err == nil {
		t.Fatal("invalid graph was accepted")
	}
	codes := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		codes[item.Code] = true
	}
	if !codes["job.duplicate-node"] || !codes["job.duplicate-mapping"] {
		t.Fatalf("graph diagnostics = %v", err)
	}
}

func TestGraphRejectsDuplicateEdgesAndUnknownNodes(t *testing.T) {
	source := NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch())
	sink := NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch())
	edge := Connect(At("source", "out"), At("sink", "in"))
	_, err := NewGraph(
		[]Node{source, sink},
		[]Edge{
			edge,
			edge,
			Connect(At("missing", "out"), At("sink", "in")),
		},
	)
	if err == nil {
		t.Fatal("invalid edges were accepted")
	}
	codes := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		codes[item.Code] = true
	}
	if !codes["job.duplicate-edge"] || !codes["job.unknown-node"] {
		t.Fatalf("graph diagnostics = %v", err)
	}
}

func TestGraphCopiesCallerSlices(t *testing.T) {
	nodes := []Node{
		NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch()),
		NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch()),
	}
	edges := []Edge{Connect(At("source", "out"), At("sink", "in"))}
	graph, err := NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	nodes[0] = Node{}
	edges[0] = Edge{}
	if !graph.Nodes()[0].Valid() || !graph.Edges()[0].Valid() {
		t.Fatal("graph retained caller slice storage")
	}
}

func TestJobExpandsDefaultPolicyAndOwnsPlannerBudget(t *testing.T) {
	graph, err := NewGraph([]Node{
		NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch()),
		NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch()),
	}, []Edge{Connect(At("source", "out"), At("sink", "in"))})
	if err != nil {
		t.Fatal(err)
	}
	request, err := New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	policy := request.Policy()
	if policy.Preset != Fast || policy.Goal != ThroughputGoal || policy.Repeatability != Repeatable || policy.Artifact != ArtifactNone || !policy.Implementation.PureGo || !policy.Implementation.SIMD || policy.Continuity != PreserveContinuity || policy.Resources.Queue != (QueuePolicy{Items: 4}) {
		t.Fatalf("default policy = %#v", policy)
	}
	if request.Budget() != DefaultBudget() {
		t.Fatalf("default budget = %#v", request.Budget())
	}

	portable, ok := PolicyFor(Portable)
	if !ok {
		t.Fatal("portable policy did not expand")
	}
	realtime, ok := PolicyFor(Realtime)
	if !ok || realtime.Resources.Queue != (QueuePolicy{Items: 2, Bytes: 16 << 20, Window: 250 * time.Millisecond}) {
		t.Fatalf("realtime queue policy = %#v, %v", realtime.Resources.Queue, ok)
	}
	budget := Budget{States: 7, Compiles: 11, SuggestionsPerNeed: 2, FixpointIterations: 3}
	request, err = New(nil, nil, graph, WithPolicy(portable), WithBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	if request.Policy() != portable || request.Budget() != budget {
		t.Fatalf("configured planning requirements = %#v %#v", request.Policy(), request.Budget())
	}
	if _, err := New(nil, nil, graph, WithPolicy(Policy{})); err == nil {
		t.Fatal("invalid policy was accepted")
	}
	if _, err := New(nil, nil, graph, WithBudget(Budget{})); err == nil {
		t.Fatal("invalid budget was accepted")
	}
}

func TestJobReportsEveryInvalidPolicyDimension(t *testing.T) {
	graph, err := NewGraph([]Node{
		NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch()),
		NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch()),
	}, []Edge{Connect(At("source", "out"), At("sink", "in"))})
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{}
	policy.Resources.Queue.Bytes = resource.Bytes(1 << 63)
	policy.Resources.Queue.Window = -time.Nanosecond
	_, err = New(nil, nil, graph, WithPolicy(policy))
	if err == nil {
		t.Fatal("invalid policy was accepted")
	}
	want := map[string]string{
		"job.invalid-policy-preset":         "policy.preset",
		"job.invalid-policy-goal":           "policy.goal",
		"job.invalid-policy-accuracy":       "policy.accuracy",
		"job.invalid-policy-repeatability":  "policy.repeatability",
		"job.invalid-policy-artifact":       "policy.artifact",
		"job.invalid-policy-implementation": "policy.implementation",
		"job.invalid-policy-continuity":     "policy.continuity",
		"job.invalid-policy-queue-items":    "policy.resources.queue.items",
		"job.invalid-policy-queue-bytes":    "policy.resources.queue.bytes",
		"job.invalid-policy-queue-window":   "policy.resources.queue.window",
	}
	items := diagnostic.ItemsOf(err)
	if len(items) != len(want) {
		t.Fatalf("policy diagnostics = %#v", items)
	}
	for _, item := range items {
		path, ok := want[item.Code]
		if !ok || item.Path.String() != path {
			t.Fatalf("unexpected policy diagnostic = %#v", item)
		}
		delete(want, item.Code)
	}
	if len(want) != 0 {
		t.Fatalf("missing policy diagnostics = %v", want)
	}
}
