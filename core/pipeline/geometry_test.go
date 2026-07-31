package pipeline

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/godexture/godec/core/node"
)

func TestGeometryCloseReleasesAbandonedNodes(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var order []int
	geometry := NewGeometry()
	for i := range 3 {
		index := i
		if err := geometry.AddNode(string(rune('a'+i)), &lifecycleTestNode{onClose: func() {
			mu.Lock()
			order = append(order, index)
			mu.Unlock()
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := geometry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := geometry.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got, want := order, []int{2, 1, 0}; !slices.Equal(got, want) {
		t.Fatalf("close order = %v, want %v", got, want)
	}
}

type stagedTestNode struct {
	lifecycleTestNode
	preload func(context.Context) error
}

func (n *stagedTestNode) InputPhases() map[string]node.InputPhase {
	return map[string]node.InputPhase{"in": node.InputPhaseRun, "ir": node.InputPhasePreload}
}

func (n *stagedTestNode) Preload(ctx context.Context) error {
	if n.preload == nil {
		return nil
	}
	return n.preload(ctx)
}

func TestPlanPreparationSeparatesAuxiliaryPath(t *testing.T) {
	t.Parallel()
	aux := &lifecycleTestNode{}
	consumer := &stagedTestNode{}
	definitions := []NodeDef{{ID: "aux", Node: aux}, {ID: "consumer", Node: consumer}}
	edges := []EdgeDef{{FromNode: "aux", FromPort: "out", ToNode: "consumer", ToPort: "ir"}}
	plan, err := planPreparation(definitions, edges, map[string]node.Node{"aux": aux, "consumer": consumer})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.nodes), 1; got != want || plan.nodes[0] != aux {
		t.Fatalf("prepare nodes = %#v, want auxiliary node", plan.nodes)
	}
	if got, want := len(plan.preloads), 1; got != want || plan.preloads[0] != consumer {
		t.Fatalf("preload nodes = %#v, want consumer", plan.preloads)
	}
	if got, want := len(plan.run), 1; got != want || plan.run[0] != consumer {
		t.Fatalf("run nodes = %#v, want consumer", plan.run)
	}
}

func TestBuilderTransfersOwnershipToPipeline(t *testing.T) {
	t.Parallel()
	owned := &lifecycleTestNode{}
	geometry := NewGeometry()
	if err := geometry.AddNode("node", owned); err != nil {
		t.Fatal(err)
	}
	pipeline, err := NewBuilder().Build(geometry)
	if err != nil {
		t.Fatal(err)
	}
	if err := geometry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := owned.closeCalls(); got != 0 {
		t.Fatalf("geometry closed transferred node %d times", got)
	}
	if err := pipeline.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := owned.closeCalls(); got != 1 {
		t.Fatalf("pipeline closed node %d times, want 1", got)
	}
}

func TestBuilderClosesNodesWhenLinkingFails(t *testing.T) {
	t.Parallel()
	first := &lifecycleTestNode{}
	second := &lifecycleTestNode{}
	geometry := NewGeometry()
	if err := geometry.AddNode("first", first); err != nil {
		t.Fatal(err)
	}
	if err := geometry.AddNode("second", second); err != nil {
		t.Fatal(err)
	}
	if err := geometry.AddEdge("first", "out", "missing", "in"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBuilder().Build(geometry); !errors.Is(err, ErrInvalidPipeline) {
		t.Fatalf("Build() error = %v, want ErrInvalidPipeline", err)
	}
	if first.closeCalls() != 1 || second.closeCalls() != 1 {
		t.Fatalf("close calls = (%d, %d), want (1, 1)", first.closeCalls(), second.closeCalls())
	}
}

func TestGeometryRejectsDuplicateNodeID(t *testing.T) {
	t.Parallel()
	geometry := NewGeometry()
	if err := geometry.AddNode("node", &lifecycleTestNode{}); err != nil {
		t.Fatal(err)
	}
	if err := geometry.AddNode("node", &lifecycleTestNode{}); !errors.Is(err, ErrInvalidPipeline) {
		t.Fatalf("duplicate AddNode() error = %v, want ErrInvalidPipeline", err)
	}
	if err := geometry.Close(); err != nil {
		t.Fatal(err)
	}
}
