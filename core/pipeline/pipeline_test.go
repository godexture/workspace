package pipeline

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
)

type lifecycleTestNode struct {
	start     func(context.Context) error
	closeErr  error
	closeMu   sync.Mutex
	closeCall int
	onClose   func()
}

type preparingTestNode struct {
	lifecycleTestNode
	prepare func(registry.ResourceGrant) error
}

func (n *preparingTestNode) Prepare(grant registry.ResourceGrant) error {
	if n.prepare == nil {
		return nil
	}
	return n.prepare(grant)
}

func (n *lifecycleTestNode) Start(ctx context.Context) error {
	if n.start == nil {
		return nil
	}
	return n.start(ctx)
}

func (n *lifecycleTestNode) Close() error {
	n.closeMu.Lock()
	defer n.closeMu.Unlock()
	n.closeCall++
	if n.onClose != nil {
		n.onClose()
	}
	return n.closeErr
}

func (n *lifecycleTestNode) closeCalls() int {
	n.closeMu.Lock()
	defer n.closeMu.Unlock()
	return n.closeCall
}

func TestPipelineRunClosesNodesInReverseOrderExactlyOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var order []int
	nodes := make([]*lifecycleTestNode, 3)
	owned := make([]node.Node, len(nodes))
	for i := range nodes {
		index := i
		nodes[i] = &lifecycleTestNode{onClose: func() {
			mu.Lock()
			order = append(order, index)
			mu.Unlock()
		}}
		owned[i] = nodes[i]
	}
	pipeline, err := New(owned...)
	if err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got, want := order, []int{2, 1, 0}; !slices.Equal(got, want) {
		t.Fatalf("close order = %v, want %v", got, want)
	}
	for i, current := range nodes {
		if got := current.closeCalls(); got != 1 {
			t.Fatalf("node %d closed %d times, want 1", i, got)
		}
	}
}

func TestPipelineCloseCancelsRunningNodesAndWaits(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	node := &lifecycleTestNode{start: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	pipeline, err := New(node)
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() {
		runDone <- pipeline.Run(context.Background())
	}()
	<-started

	if err := pipeline.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Close")
	}
	if got := node.closeCalls(); got != 1 {
		t.Fatalf("node closed %d times, want 1", got)
	}
}

func TestPipelineReturnsRunAndCloseErrors(t *testing.T) {
	t.Parallel()
	runErr := errors.New("run")
	closeErr := errors.New("close")
	pipeline, err := New(&lifecycleTestNode{
		start:    func(context.Context) error { return runErr },
		closeErr: closeErr,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pipeline.Run(context.Background())
	if !errors.Is(err, runErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Run() error = %v, want both lifecycle errors", err)
	}
}

func TestPipelineIsSingleUse(t *testing.T) {
	t.Parallel()
	pipeline, err := New(&lifecycleTestNode{})
	if err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Run(context.Background()); !errors.Is(err, ErrInvalidPipeline) {
		t.Fatalf("second Run() error = %v, want ErrInvalidPipeline", err)
	}
}

func TestPipelinePrepareWaitsForConcurrentCaller(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	pipeline, err := New(&preparingTestNode{prepare: func(registry.ResourceGrant) error {
		calls.Add(1)
		close(started)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- pipeline.Prepare(context.Background()) }()
	<-started
	go func() { second <- pipeline.Prepare(context.Background()) }()
	select {
	case err := <-second:
		t.Fatalf("concurrent Prepare() returned early: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Prepare() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Prepare() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Prepare() calls = %d, want 1", got)
	}
	if err := pipeline.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewPipelineClosesValidNodesWhenInputContainsNil(t *testing.T) {
	t.Parallel()
	first := &lifecycleTestNode{}
	last := &lifecycleTestNode{}
	if _, err := New(first, nil, last); !errors.Is(err, ErrInvalidPipeline) {
		t.Fatalf("New() error = %v, want ErrInvalidPipeline", err)
	}
	if first.closeCalls() != 1 || last.closeCalls() != 1 {
		t.Fatalf("close calls = (%d, %d), want (1, 1)", first.closeCalls(), last.closeCalls())
	}
}
