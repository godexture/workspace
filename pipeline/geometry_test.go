package pipeline

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
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
