package run

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

// namedFlushProcessor fails its Flush with an error of its own, so two of them
// in one chain are two independent failures rather than two readings of one.
type namedFlushProcessor struct {
	templateOperator
	failure error
	panic   bool
}

func (p *namedFlushProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	var item flow.Item[int]
	output.Own(&item, input.Value())
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (p *namedFlushProcessor) Flush(context.Context, flow.Emitter[int]) error {
	if p.panic {
		panic(p.failure)
	}
	return p.failure
}

// Two components that each fail to flush are two failures. Joining them into
// one error before the ledger saw them would make one event that no consumer
// can take apart, and Result.Secondary exists precisely to report the second
// one as the independent thing it is.
func TestTwoStagesThatFailToFlushAreTwoEvents(t *testing.T) {
	type chainID struct{}
	typ := schema.Define[chainID](schema.Traits[int]{Size: func(int) int { return 1 }})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "first", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "second", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("first", "in")),
			job.Connect(job.At("first", "out"), job.At("second", "in")),
			job.Connect(job.At("second", "out"), job.At("sink", "in")),
		},
		job.QueuePolicy{Items: 4},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream := errors.New("the first stage could not flush")
	downstream := errors.New("the second stage could not flush")
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2}},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: upstream},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: downstream},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	assertEachRecordedOnce(t, value, upstream, downstream)
	assertAttributedSeparately(t, value, map[error]string{upstream: "first", downstream: "second"})
}

func TestPanickingLinearFlushStillClosesDownstream(t *testing.T) {
	typ := flushType()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template := flushTemplate(t,
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "first", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "second", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("first", "in")),
			job.Connect(job.At("first", "out"), job.At("second", "in")),
			job.Connect(job.At("second", "out"), job.At("sink", "in")),
		},
	)
	downstream := errors.New("downstream flush error")
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: errors.New("first flush panic"), panic: true},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: downstream},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	assertFlushKindAt(t, value, "first", journal.WorkPanic)
	assertFlushKindAt(t, value, "second", journal.WorkError)
	assertEachRecordedOnce(t, value, downstream)
}

// The same holds across fan-out branches: one branch failing to close never
// hides another's failure, and the two are not merged into one event.
func TestTwoFanOutBranchesThatFailToFlushAreTwoEvents(t *testing.T) {
	type branchID struct{}
	typ := schema.Define[branchID](schema.Traits[int]{
		Fork: func(value int) int { return value },
		Size: func(int) int { return 1 },
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "left", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "right", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "leftSink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
			{ID: "rightSink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("left", "in")),
			job.Connect(job.At("source", "out"), job.At("right", "in")),
			job.Connect(job.At("left", "out"), job.At("leftSink", "in")),
			job.Connect(job.At("right", "out"), job.At("rightSink", "in")),
		},
		job.QueuePolicy{Items: 4},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	left := errors.New("the left branch could not flush")
	right := errors.New("the right branch could not flush")
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2}},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: left},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: right},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	assertEachRecordedOnce(t, value, left, right)
	assertAttributedSeparately(t, value, map[error]string{left: "left", right: "right"})
}

func TestPanickingFanoutBranchStillClosesItsSiblings(t *testing.T) {
	typ := forkType()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template := flushTemplate(t,
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "left", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "right", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "leftSink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
			{ID: "rightSink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("left", "in")),
			job.Connect(job.At("source", "out"), job.At("right", "in")),
			job.Connect(job.At("left", "out"), job.At("leftSink", "in")),
			job.Connect(job.At("right", "out"), job.At("rightSink", "in")),
		},
	)
	right := errors.New("right branch flush error")
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: errors.New("left branch panic"), panic: true},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: right},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	assertFlushKindAt(t, value, "left", journal.WorkPanic)
	assertFlushKindAt(t, value, "right", journal.WorkError)
	assertEachRecordedOnce(t, value, right)
}

type flushJoiner struct {
	templateOperator
	failure error
	panic   bool
}

func (j *flushJoiner) Process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int]) error {
	total := 0
	for index := range batch.Len() {
		if value, ok := batch.Value(index); ok {
			total += value
		}
	}
	var item flow.Item[int]
	output.Own(&item, total)
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (j *flushJoiner) Flush(context.Context, flow.Emitter[int]) error {
	if j.panic {
		panic(j.failure)
	}
	return j.failure
}

func TestPanickingJoinFlushStillClosesDownstream(t *testing.T) {
	typ := flushType()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	joinShape := flow.NewShape([]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))}, []flow.Port{flow.Out("out", typ)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template := flushTemplate(t,
		[]Node{
			{ID: "a", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "b", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
			{ID: "downstream", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("a", "out"), job.At("join", "in")),
			job.Connect(job.At("b", "out"), job.At("join", "in")),
			job.Connect(job.At("join", "out"), job.At("downstream", "in")),
			job.Connect(job.At("downstream", "out"), job.At("sink", "in")),
		},
	)
	downstream := errors.New("downstream flush error")
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{2}},
		&flushJoiner{templateOperator: templateOperator{shape: joinShape}, failure: errors.New("join flush panic"), panic: true},
		&namedFlushProcessor{templateOperator: templateOperator{shape: passShape}, failure: downstream},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	assertFlushKindAt(t, value, "join", journal.WorkPanic)
	assertFlushKindAt(t, value, "downstream", journal.WorkError)
	assertEachRecordedOnce(t, value, downstream)
}

func flushType() schema.Type[int] {
	type flushID struct{}
	return schema.Define[flushID](schema.Traits[int]{Size: func(int) int { return 1 }})
}

func forkType() schema.Type[int] {
	type forkID struct{}
	return schema.Define[forkID](schema.Traits[int]{Fork: func(value int) int { return value }, Size: func(int) int { return 1 }})
}

func flushTemplate(t testing.TB, nodes []Node, edges []job.Edge) Template {
	t.Helper()
	template, err := compileFixture(nodes, edges, job.QueuePolicy{Items: 4}, job.AlignmentPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	return template
}

func assertFlushKindAt(t testing.TB, value *island, node string, kind journal.Kind) {
	t.Helper()
	count := 0
	for _, event := range value.events() {
		if event.Node == node && event.Kind == kind && event.Operation == journal.Flush {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s %s flush events = %d, want one: %#v", node, kind, count, value.events())
	}
}

func assertEachRecordedOnce(t testing.TB, value *island, failures ...error) {
	t.Helper()
	for _, failure := range failures {
		count := 0
		for _, event := range value.events() {
			if errors.Is(event.Err, failure) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%v was recorded %d times, want once: %#v", failure, count, value.events())
		}
	}
}

// assertAttributedSeparately fixes that each failure kept its own node, which
// a joined error could not have carried.
func assertAttributedSeparately(t testing.TB, value *island, want map[error]string) {
	t.Helper()
	for failure, node := range want {
		found := false
		for _, event := range value.events() {
			if errors.Is(event.Err, failure) {
				found = true
				if event.Node != node {
					t.Errorf("%v was attributed to %q, want %q", failure, event.Node, node)
				}
				if event.Operation != journal.Flush {
					t.Errorf("%v has operation %v, want Flush", failure, event.Operation)
				}
			}
		}
		if !found {
			t.Errorf("%v never reached the ledger", failure)
		}
	}
}
