package drive

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type (
	driveInputID  struct{}
	driveOutputID struct{}
)

type owned struct{ value int }

type ownership struct {
	forks atomic.Int32
	drops atomic.Int32
}

func ownedSchema[ID any](state *ownership) schema.Type[owned] {
	return schema.Define[ID](schema.Traits[owned]{
		Fork: func(value owned) owned {
			state.forks.Add(1)
			return value
		},
		Drop: func(owned) { state.drops.Add(1) },
		Size: func(owned) int { return 1 },
		Time: func(value owned) (int64, bool) { return int64(value.value), true },
	})
}

type operatorBase struct{ shape flow.Shape }

func (o operatorBase) Ports() flow.Shape { return o.shape.Clone() }
func (operatorBase) Close() error        { return nil }

type sliceReader struct {
	operatorBase
	typ    schema.Type[owned]
	values []owned
	index  int
}

func (r *sliceReader) Read(_ context.Context, into *flow.Item[owned]) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	into.Set(r.values[r.index])
	r.index++
	return nil
}

type mapProcessor struct {
	operatorBase
	input  schema.Type[owned]
	output schema.Type[owned]
	flush  atomic.Int32
}

func (p *mapProcessor) Process(ctx context.Context, input *flow.Item[owned], output flow.Emitter[owned]) error {
	defer input.Drop()
	item := flow.NewItem(owned{value: input.Value().value + 10}, p.output, &testDomain)
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (p *mapProcessor) Flush(context.Context, flow.Emitter[owned]) error {
	p.flush.Add(1)
	return nil
}

type recordingWriter struct {
	operatorBase
	mu      sync.Mutex
	values  []int
	failure error
}

func (w *recordingWriter) Write(_ context.Context, input *flow.Item[owned]) error {
	if w.failure != nil {
		return w.failure
	}
	w.mu.Lock()
	w.values = append(w.values, input.Value().value)
	w.mu.Unlock()
	input.Drop()
	return nil
}

func (w *recordingWriter) Values() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]int(nil), w.values...)
}

func TestTypedSourceProcessorSinkComposeWithoutPerItemErasure(t *testing.T) {
	inputOwners := &ownership{}
	outputOwners := &ownership{}
	inputType := ownedSchema[driveInputID](inputOwners)
	outputType := ownedSchema[driveOutputID](outputOwners)
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", inputType)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", inputType)}, []flow.Port{flow.Out("out", outputType)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", outputType)}, nil)
	source := &sliceReader{operatorBase: operatorBase{sourceShape}, typ: inputType, values: []owned{{1}, {2}, {3}}}
	processor := &mapProcessor{operatorBase: operatorBase{processorShape}, input: inputType, output: outputType}
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}

	sourceBinding := NewSource("out", inputType)
	processorBinding := NewProcessor("in", inputType, "out", outputType)
	sinkBinding := NewSink("in", outputType)
	for _, value := range []struct {
		binding Binding
		shape   flow.Shape
	}{{sourceBinding, sourceShape}, {processorBinding, processorShape}, {sinkBinding, sinkShape}} {
		if err := value.binding.Validate(value.shape); err != nil {
			t.Fatal(err)
		}
	}

	sinkLink, err := sinkBinding.OpenSink(sink)
	if err != nil {
		t.Fatal(err)
	}
	processorLink, err := processorBinding.Prepend(processor, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("source")
	task, err := sourceBinding.OpenSource(source, processorLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	requireNoFailures(t, ledger)
	if err := task.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sink.Values(); len(got) != 3 || got[0] != 11 || got[1] != 12 || got[2] != 13 {
		t.Fatalf("sink values = %v", got)
	}
	if processor.flush.Load() != 1 {
		t.Fatalf("processor flush count = %d", processor.flush.Load())
	}
	if inputOwners.forks.Load() != 0 || outputOwners.forks.Load() != 0 {
		t.Fatalf("linear path forks = input %d output %d", inputOwners.forks.Load(), outputOwners.forks.Load())
	}
	if inputOwners.drops.Load() != 3 || outputOwners.drops.Load() != 3 {
		t.Fatalf("linear drops = input %d output %d", inputOwners.drops.Load(), outputOwners.drops.Load())
	}
}

func TestFanoutRetainsRollbackOwnerAcrossPartialFailure(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &recordingWriter{operatorBase: operatorBase{shape}}
	want := errors.New("sink rejected item")
	right := &recordingWriter{operatorBase: operatorBase{shape}, failure: want}
	sinkBinding := NewSink("in", typ)
	leftLink, _ := sinkBinding.OpenSink(left)
	rightLink, _ := sinkBinding.OpenSink(right)
	sourceBinding := NewSource("out", typ)
	fanout, err := sourceBinding.Fanout([]Link{leftLink, rightLink}, "node")
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("fanout")
	fanout.bind(owner)
	target, err := deliveryOf[owned](fanout)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewItem(owned{value: 9}, typ, &testDomain)
	err = owner.Perform(journal.Run, func(*journal.Span) error {
		return target.Emit(context.Background(), &input)
	})
	if !errors.Is(err, want) {
		t.Fatalf("fan-out error = %v", err)
	}
	input.Drop()
	if owners.forks.Load() != 1 || owners.drops.Load() != 2 {
		t.Fatalf("fan-out ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
	if got := left.Values(); len(got) != 1 || got[0] != 9 || len(right.Values()) != 0 {
		t.Fatalf("fan-out values = left %v right %v", got, right.Values())
	}
	if events := ledger.Events(); len(events) != 1 || events[0].Kind != journal.WorkError {
		t.Fatalf("fan-out failures = %#v, want one callback failure", events)
	}
}

func TestOneOutputFanoutIsLinearMove(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{shape}}
	link, _ := NewSink("in", typ).OpenSink(sink)
	linear, err := NewSource("out", typ).Fanout([]Link{link}, "node")
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("fanout")
	linear.bind(owner)
	target, _ := deliveryOf[owned](linear)
	item := flow.NewItem(owned{value: 4}, typ, &testDomain)
	defer item.Drop()
	if err := target.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	if owners.forks.Load() != 0 || owners.drops.Load() != 1 {
		t.Fatalf("one-output ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

type editingFrameWriter struct {
	operatorBase
	allocator *buffer.Allocator
	copies    int
	value     int16
}

func (w *editingFrameWriter) Write(_ context.Context, input *flow.Item[audio.Frame[int16]]) error {
	edit, err := input.Value().Edit(w.allocator)
	if err != nil {
		return err
	}
	defer edit.Discard()
	if edit.Copied() {
		w.copies++
	}
	samples, err := edit.PlaneSamples(0)
	if err != nil {
		return err
	}
	samples[0] = 9
	candidate := edit.Frame()
	w.value = samples[0]
	if err := edit.Commit(); err != nil {
		return err
	}
	candidate.Release()
	return nil
}

type readingFrameWriter struct {
	operatorBase
	value int16
}

func (w *readingFrameWriter) Write(_ context.Context, input *flow.Item[audio.Frame[int16]]) error {
	samples, err := input.Value().PlaneSamples(0)
	if err != nil {
		return err
	}
	copy := samples.AppendTo(nil)
	copy[0] = 7
	w.value = samples.At(0)
	input.Drop()
	return nil
}

func TestAudioFanoutCopiesOnlyModifyingBranch(t *testing.T) {
	type audioFanoutID struct{}
	allocator, err := buffer.NewAllocator(64)
	if err != nil {
		t.Fatal(err)
	}
	planes, err := allocator.FromBytes([]byte{0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := audio.NewFrame[int16](timing.UnknownPTS(), 1, planes)
	if err != nil {
		t.Fatal(err)
	}
	typ := schema.Define[audioFanoutID](schema.Traits[audio.Frame[int16]]{
		Fork: func(value audio.Frame[int16]) audio.Frame[int16] { return value.Share() },
		Drop: func(value audio.Frame[int16]) { value.Release() },
	})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &editingFrameWriter{operatorBase: operatorBase{shape: shape}, allocator: allocator}
	right := &readingFrameWriter{operatorBase: operatorBase{shape: shape}}
	sink := NewSink("in", typ)
	leftLink, err := sink.OpenSink(left)
	if err != nil {
		t.Fatal(err)
	}
	rightLink, err := sink.OpenSink(right)
	if err != nil {
		t.Fatal(err)
	}
	fanout, err := NewSource("out", typ).Fanout([]Link{leftLink, rightLink}, "node")
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("fanout")
	fanout.bind(owner)
	target, err := deliveryOf[audio.Frame[int16]](fanout)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(frame, typ, &testDomain)
	defer item.Drop()
	if err := target.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	if left.copies != 1 || left.value != 9 || right.value != 0 {
		t.Fatalf("fan-out edit = copies %d modified %d read-only %d", left.copies, left.value, right.value)
	}
	if allocator.Used() != 0 {
		t.Fatalf("fan-out edit retained %d bytes", allocator.Used())
	}
}

func TestBufferedLinkDrainsInOrderAndClosesDownstream(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	sinkLink, _ := NewSink("in", typ).OpenSink(sink)
	ledger := journal.NewLedger()
	buffered, bufferTask, err := NewSource("out", typ).Buffer(queue.Limit{Items: 2, Bytes: 2, Span: 10}, sinkLink, ledger.Domain("edge", "edge"))
	if err != nil {
		t.Fatal(err)
	}
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	source := &sliceReader{operatorBase: operatorBase{sourceShape}, typ: typ, values: []owned{{1}, {2}, {3}}}
	sourceTask, err := NewSource("out", typ).OpenSource(source, buffered, ledger.Domain("source", "source"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- perform(ctx, bufferTask) }()
	go func() { results <- perform(ctx, sourceTask) }()
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if err := sourceTask.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	requireNoFailures(t, ledger)
	if got := sink.Values(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("buffered values = %v", got)
	}
	if owners.forks.Load() != 0 || owners.drops.Load() != 3 {
		t.Fatalf("buffer ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

func TestBufferedLinkCanceledAfterProducerSealDoesNotFlush(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	sinkLink, err := NewSink("in", typ).OpenSink(sink)
	if err != nil {
		t.Fatal(err)
	}
	processorShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ)},
		[]flow.Port{flow.Out("out", typ)},
	)
	processor := &mapProcessor{operatorBase: operatorBase{processorShape}, input: typ, output: typ}
	processorLink, err := NewProcessor("in", typ, "out", typ).Prepend(processor, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("edge")
	buffered, bufferTask, err := NewSource("out", typ).Buffer(queue.Limit{Items: 2}, processorLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	// Seal is the producer's successful EOF signal. Cancel after the seal but
	// before the drain task starts so its prepared phase context carries the
	// external cancellation and cannot start Flush.
	if err := buffered.Close(ctx); err != nil {
		t.Fatal(err)
	}
	want := errors.New("run stopped after producer EOF")
	cancel(want)
	if err := perform(ctx, bufferTask); !errors.Is(err, want) {
		t.Fatalf("buffer task cause = %v, want %v", err, want)
	}
	if got := processor.flush.Load(); got != 0 {
		t.Fatalf("Flush calls after cancellation = %d, want none", got)
	}
	for _, event := range ledger.Events() {
		if event.Operation == journal.Flush {
			t.Fatalf("canceled sealed edge recorded Flush event: %#v", event)
		}
	}
}

type panickingWriter struct{ operatorBase }

func (panickingWriter) Write(context.Context, *flow.Item[owned]) error { panic("writer panicked") }

// decliningWriter leaves the cell full, which the ownership rule allows: Emit
// offers a value, it does not hand it over.
type decliningWriter struct{ operatorBase }

func (decliningWriter) Write(context.Context, *flow.Item[owned]) error { return nil }

// assertSettledButNotQuiescent fixes what an edge owes after its drain task
// stopped over a value it never finished. The count is settled, because nothing
// is being processed. Quiescence is not reported, because that value did not
// reach the sink and a barrier claiming otherwise would let the caller run
// Finalize over a dead data path. The failure that stopped the task is what
// ends the wait.
func assertSettledButNotQuiescent(t *testing.T, producer delivery[owned], task Task) {
	t.Helper()
	if snapshot := producer.(*bufferDelivery[owned]).queue.Snapshot(); snapshot.Active != 0 || snapshot.Items != 0 {
		t.Fatalf("edge after an unfinished value = %#v", snapshot)
	}
	waiting, giveUp := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer giveUp()
	if err := task.Barrier(waiting); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("barrier over an unfinished value = %v", err)
	}
	failed := errors.New("the consumer stopped")
	cancelled, stop := context.WithCancelCause(context.Background())
	stop(failed)
	defer stop(nil)
	if err := task.Barrier(cancelled); !errors.Is(err, failed) {
		t.Fatalf("barrier cause = %v, want the failure that stopped the consumer", err)
	}
}

// A value is finished only once the consumer has had its chance at it and
// whatever the consumer declined has been released. Every way of falling short
// of that leaves the same obligation, so an error is not a lighter case than a
// panic.
func TestAnUnfinishedValueSettlesTheEdgeWithoutQuiescingIt(t *testing.T) {
	for _, test := range []struct {
		name       string
		dropPanics bool
		writer     func(flow.Shape) flow.Operator
	}{
		{
			name: "the consumer reports a failure",
			writer: func(shape flow.Shape) flow.Operator {
				return &recordingWriter{operatorBase: operatorBase{shape}, failure: errors.New("sink failure")}
			},
		},
		{
			name:   "the consumer panics",
			writer: func(shape flow.Shape) flow.Operator { return &panickingWriter{operatorBase{shape}} },
		},
		{
			name:       "the declined payload cannot be released",
			dropPanics: true,
			writer:     func(shape flow.Shape) flow.Operator { return &decliningWriter{operatorBase{shape}} },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var released atomic.Int32
			typ := schema.Define[driveOutputID](schema.Traits[owned]{
				Drop: func(owned) {
					released.Add(1)
					if test.dropPanics {
						panic("declared drop panicked")
					}
				},
				Size: func(owned) int { return 1 },
			})
			sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
			sinkLink, err := NewSink("in", typ).OpenSink(test.writer(sinkShape))
			if err != nil {
				t.Fatal(err)
			}
			ledger, edge := testOwner("edge")
			buffered, bufferTask, err := NewSource("out", typ).Buffer(queue.Limit{Items: 2}, sinkLink, edge)
			if err != nil {
				t.Fatal(err)
			}
			producer, err := deliveryOf[owned](buffered)
			if err != nil {
				t.Fatal(err)
			}
			// The producer's end of the edge belongs to whoever fills it, which
			// here is the test rather than another task.
			producerLedger := journal.NewLedger()
			buffered.bind(producerLedger.Domain("producer", "producer"))
			item := flow.NewItem(owned{value: 1}, typ, &testDomain)
			if err := producer.Emit(context.Background(), &item); err != nil {
				t.Fatal(err)
			}
			_ = perform(context.Background(), bufferTask)
			assertSettledButNotQuiescent(t, producer, bufferTask)
			if released.Load() != 1 {
				t.Fatalf("releases of the unfinished value = %d", released.Load())
			}
			if test.dropPanics != (len(cleanups(ledger)) == 1) {
				t.Fatalf("releases reported to the edge = %#v", cleanups(ledger))
			}
		})
	}
}

type intOperator struct {
	operatorBase
	out flow.Item[int]
}

func (p *intOperator) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	output.Own(&p.out, input.Value()+1)
	defer p.out.Drop()
	return output.Emit(ctx, &p.out)
}

func (*intOperator) Flush(context.Context, flow.Emitter[int]) error { return nil }

type intWriter struct {
	operatorBase
	value int
}

func (w *intWriter) Write(_ context.Context, input *flow.Item[int]) error {
	w.value = input.Value()
	return nil
}

func TestLinearProcessorHopAllocatesZero(t *testing.T) {
	type inputID struct{}
	type outputID struct{}
	in := schema.Define[inputID](schema.Traits[int]{})
	out := schema.Define[outputID](schema.Traits[int]{})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", in)}, []flow.Port{flow.Out("out", out)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	writer := &intWriter{operatorBase: operatorBase{sinkShape}}
	next, err := NewSink("in", out).OpenSink(writer)
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewProcessor("in", in, "out", out).Prepend(&intOperator{operatorBase: operatorBase{processorShape}}, next)
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[int](link)
	if err != nil {
		t.Fatal(err)
	}
	var cell flow.Item[int]
	defer cell.Drop()
	allocations := testing.AllocsPerRun(1000, func() {
		target.Own(&cell, 1)
		if err := target.Emit(context.Background(), &cell); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("linear processor hop allocations = %v", allocations)
	}
}

func TestBindingRejectsWrongShapeOperatorAndPayload(t *testing.T) {
	typ := schema.Define[driveInputID](schema.Traits[int]{})
	other := schema.Define[driveOutputID](schema.Traits[string]{})
	binding := NewSource("out", typ)
	wrongShape := flow.NewShape(nil, []flow.Port{flow.Out("other", typ)})
	if err := binding.Validate(wrongShape); !errors.Is(err, ErrBinding) {
		t.Fatalf("shape error = %v", err)
	}
	operator := operatorBase{shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})}
	if err := binding.ValidateOperator(operator); !errors.Is(err, ErrOperator) {
		t.Fatalf("operator error = %v", err)
	}
	otherSink := &recordingWriter{operatorBase: operatorBase{flow.NewShape([]flow.Port{flow.In("in", ownedSchema[driveOutputID](&ownership{}))}, nil)}}
	otherLink, _ := NewSink("in", ownedSchema[driveOutputID](&ownership{})).OpenSink(otherSink)
	_, owner := testOwner("source")
	if _, err := NewSource("out", typ).OpenSource(&intReader{operatorBase: operatorBase{operator.Ports()}, typ: typ}, otherLink, owner); !errors.Is(err, ErrLink) {
		t.Fatalf("payload link error = %v", err)
	}
	// A task cannot be constructed without the failure domain it and its slots
	// report to, so there is no shape of this call that produces one reporting
	// into an anonymous journal nobody collects.
	if _, err := NewSource("out", typ).OpenSource(&intReader{operatorBase: operatorBase{operator.Ports()}, typ: typ}, otherLink, nil); !errors.Is(err, ErrDomain) {
		t.Fatalf("missing domain error = %v", err)
	}
	_ = other
}

type intReader struct {
	operatorBase
	typ schema.Type[int]
}

func (r *intReader) Read(_ context.Context, into *flow.Item[int]) error {
	into.Set(1)
	return nil
}

type sumJoiner struct {
	operatorBase
	output schema.Type[owned]
	flush  atomic.Int32
}

func (j *sumJoiner) Process(ctx context.Context, batch flow.Batch[owned], output flow.Emitter[owned]) error {
	left, leftOK := batch.Value(0)
	right, rightOK := batch.Value(1)
	if !leftOK || !rightOK {
		return errors.New("invalid zip batch")
	}
	item := flow.NewItem(owned{value: left.value + right.value}, j.output, &testDomain)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (j *sumJoiner) Flush(context.Context, flow.Emitter[owned]) error {
	j.flush.Add(1)
	return nil
}

func TestAbortedJoinDoesNotFlush(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	joiner := &sumJoiner{operatorBase: operatorBase{joinShape}, output: out}
	sink, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("join")
	_, task, err := NewJoiner("in", in, flow.ZipFanIn, "out", out).OpenJoiner(joiner, 2, queue.Limit{Items: 2}, 0, sink, owner)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- perform(context.Background(), task) }()
	task.Abort()
	if err := <-done; err != nil {
		t.Fatalf("aborted join run = %v", err)
	}
	if err := task.Barrier(context.Background()); err != nil {
		t.Fatalf("aborted join barrier = %v", err)
	}
	if err := task.Finish(context.Background()); err != nil {
		t.Fatalf("aborted join finish = %v", err)
	}
	if joiner.flush.Load() != 0 {
		t.Fatalf("aborted join Flush calls = %d, want none", joiner.flush.Load())
	}
}

func TestZipJoinerUsesConnectionOrderAndRuntimeOwnsBatch(t *testing.T) {
	inputOwners := &ownership{}
	outputOwners := &ownership{}
	in := ownedSchema[driveInputID](inputOwners)
	out := ownedSchema[driveOutputID](outputOwners)
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	joiner := &sumJoiner{operatorBase: operatorBase{joinShape}, output: out}
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	sinkLink, err := NewSink("in", out).OpenSink(sink)
	if err != nil {
		t.Fatal(err)
	}
	binding := NewJoiner("in", in, flow.ZipFanIn, "out", out)
	if err := binding.Validate(joinShape); err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("join")
	inputs, task, err := binding.OpenJoiner(joiner, 2, queue.Limit{Items: 2}, 0, sinkLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(inputs...)
	left, _ := deliveryOf[owned](inputs[0])
	right, _ := deliveryOf[owned](inputs[1])
	result := make(chan error, 1)
	go func() { result <- perform(context.Background(), task) }()
	for _, pair := range [][2]int{{1, 10}, {2, 20}} {
		leftItem := flow.NewItem(owned{value: pair[0]}, in, &testDomain)
		if err := left.Emit(context.Background(), &leftItem); err != nil {
			t.Fatal(err)
		}
		leftItem.Drop()
		rightItem := flow.NewItem(owned{value: pair[1]}, in, &testDomain)
		if err := right.Emit(context.Background(), &rightItem); err != nil {
			t.Fatal(err)
		}
		rightItem.Drop()
	}
	if err := left.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := right.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	quiesce, giveUp := context.WithTimeout(context.Background(), time.Second)
	defer giveUp()
	if err := task.Barrier(quiesce); err != nil {
		t.Fatalf("barrier after a drained join = %v", err)
	}
	if err := task.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sink.Values(); len(got) != 2 || got[0] != 11 || got[1] != 22 {
		t.Fatalf("zip values = %v", got)
	}
	if inputOwners.drops.Load() != 4 || outputOwners.drops.Load() != 2 || inputOwners.forks.Load() != 0 {
		t.Fatalf("zip ownership = inputs drops %d forks %d, outputs drops %d", inputOwners.drops.Load(), inputOwners.forks.Load(), outputOwners.drops.Load())
	}
	if joiner.flush.Load() != 1 {
		t.Fatalf("zip flushes = %d", joiner.flush.Load())
	}
	requireNoFailures(t, ledger)
}

type zipReleaseID struct{}

// The batch is released after every ordinary Process, and a declared Drop is
// third-party code. A release that fails there is the task's answer: reporting
// only the one on the way out would let a broken release pass as a joined
// stream.
func TestZipReportsAFailedReleaseBetweenBatches(t *testing.T) {
	in := schema.Define[zipReleaseID](schema.Traits[owned]{
		Drop: func(owned) { panic("declared drop panicked") },
		Size: func(owned) int { return 1 },
		Time: func(value owned) (int64, bool) { return int64(value.value), true },
	})
	out := ownedSchema[driveOutputID](&ownership{})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	sinkLink, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	joiner := &sumJoiner{operatorBase: operatorBase{joinShape}, output: out}
	ledger, owner := testOwner("join")
	inputs, task, err := NewJoiner("in", in, flow.ZipFanIn, "out", out).OpenJoiner(joiner, 2, queue.Limit{Items: 2}, 0, sinkLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(inputs...)
	result := make(chan error, 1)
	go func() { result <- perform(context.Background(), task) }()
	for _, input := range inputs {
		edge, err := deliveryOf[owned](input)
		if err != nil {
			t.Fatal(err)
		}
		item := flow.NewItem(owned{value: 1}, in, &testDomain)
		if err := edge.Emit(context.Background(), &item); err != nil {
			t.Fatal(err)
		}
		// Closing lets the join reach EOF, so a discarded release shows up as a
		// task that joined successfully rather than as a hang.
		if err := edge.close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Nothing stopped the work, but the task ended holding a payload it could
	// not release, so its cause is that release rather than nothing at all.
	assertCauseIsRecorded(t, ledger, <-result)
	releases := cleanups(ledger)
	if len(releases) == 0 {
		t.Fatal("the join finished without reporting the batch it could not release")
	}
	for _, failure := range releases {
		if failure.Kind != journal.CleanupPanic {
			t.Errorf("release failure kind = %v, want a cleanup panic", failure.Kind)
		}
		if strings.Contains(failure.Err.Error(), "declared drop panicked") {
			t.Error("the release report exposes the recovered panic value")
		}
	}
}

func TestZipJoinerEnforcesTimestampTolerance(t *testing.T) {
	inputOwners := &ownership{}
	outputOwners := &ownership{}
	in := ownedSchema[driveInputID](inputOwners)
	out := ownedSchema[driveOutputID](outputOwners)
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	joiner := &sumJoiner{operatorBase: operatorBase{joinShape}, output: out}
	sinkLink, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("join")
	binding := NewJoiner("in", in, flow.ZipFanIn, "out", out)
	if _, _, err := binding.OpenJoiner(joiner, 2, queue.Limit{Items: 2}, -1, sinkLink, owner); !errors.Is(err, ErrBinding) {
		t.Fatalf("negative tolerance error = %v", err)
	}
	inputs, task, err := binding.OpenJoiner(joiner, 2, queue.Limit{Items: 2, Span: 1}, 5, sinkLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(inputs...)
	result := make(chan error, 1)
	go func() { result <- perform(context.Background(), task) }()
	left, _ := deliveryOf[owned](inputs[0])
	right, _ := deliveryOf[owned](inputs[1])
	leftItem := flow.NewItem(owned{value: 1}, in, &testDomain)
	defer leftItem.Drop()
	if err := left.Emit(context.Background(), &leftItem); err != nil {
		t.Fatal(err)
	}
	rightItem := flow.NewItem(owned{value: 10}, in, &testDomain)
	defer rightItem.Drop()
	if err := right.Emit(context.Background(), &rightItem); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrTolerance) {
		t.Fatalf("tolerance error = %v", err)
	}
	task.Discard()
	if inputOwners.drops.Load() != 2 {
		t.Fatalf("tolerance input drops = %d", inputOwners.drops.Load())
	}
}

type panickingJoiner struct{ operatorBase }

func (*panickingJoiner) Process(context.Context, flow.Batch[owned], flow.Emitter[owned]) error {
	panic("joiner panicked")
}

func (*panickingJoiner) Flush(context.Context, flow.Emitter[owned]) error { return nil }

// A joiner that panics unwinds through the deferred close of the task's done
// channel. The barrier must not read that as a finished join: the batch never
// reached the sink, and claiming quiescence would let the caller run Finalize
// over a dead data path and report the panic at whichever boundary noticed the
// cancellation next.
func TestAPanickingJoinDoesNotReportABarrier(t *testing.T) {
	owners := &ownership{}
	in := ownedSchema[driveInputID](owners)
	out := ownedSchema[driveOutputID](&ownership{})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	sinkLink, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("join")
	inputs, task, err := NewJoiner("in", in, flow.ZipFanIn, "out", out).OpenJoiner(&panickingJoiner{operatorBase{joinShape}}, 2, queue.Limit{Items: 2}, 0, sinkLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(inputs...)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = perform(context.Background(), task)
	}()
	for _, input := range inputs {
		edge, err := deliveryOf[owned](input)
		if err != nil {
			t.Fatal(err)
		}
		item := flow.NewItem(owned{value: 1}, in, &testDomain)
		if err := edge.Emit(context.Background(), &item); err != nil {
			t.Fatal(err)
		}
	}
	<-stopped
	waiting, giveUp := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer giveUp()
	if err := task.Barrier(waiting); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("barrier over a panicking join = %v", err)
	}
	failed := errors.New("the join panicked")
	cancelled, stop := context.WithCancelCause(context.Background())
	stop(failed)
	defer stop(nil)
	if err := task.Barrier(cancelled); !errors.Is(err, failed) {
		t.Fatalf("barrier cause = %v, want the failure that stopped the join", err)
	}
	if owners.drops.Load() != 2 {
		t.Fatalf("join drops after a panic = %d", owners.drops.Load())
	}
}

// Reaching EOF is where a join can quiesce, not proof that it did. One input
// ending leaves the batch already popped from the others unjoined, and
// releasing it is the last thing the task does. A release that fails there is
// the task's failure, so the barrier must not answer for the run that reported
// it.
func TestAJoinThatCannotReleaseItsLastBatchDoesNotQuiesce(t *testing.T) {
	in := schema.Define[zipReleaseID](schema.Traits[owned]{
		Drop: func(owned) { panic("declared drop panicked") },
		Size: func(owned) int { return 1 },
	})
	out := ownedSchema[driveOutputID](&ownership{})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	sinkLink, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	joiner := &sumJoiner{operatorBase: operatorBase{joinShape}, output: out}
	ledger, owner := testOwner("join")
	inputs, task, err := NewJoiner("in", in, flow.ZipFanIn, "out", out).OpenJoiner(joiner, 2, queue.Limit{Items: 2}, 0, sinkLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(inputs...)
	// Only the first input carries a value, so the join pops it, finds the
	// second at EOF, and ends holding a batch it never joined.
	first, err := deliveryOf[owned](inputs[0])
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(owned{value: 1}, in, &testDomain)
	if err := first.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		edge, err := deliveryOf[owned](input)
		if err != nil {
			t.Fatal(err)
		}
		if err := edge.close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	assertCauseIsRecorded(t, ledger, perform(context.Background(), task))
	if len(cleanups(ledger)) == 0 {
		t.Fatal("the join finished without reporting the batch it could not release")
	}
	waiting, giveUp := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer giveUp()
	if err := task.Barrier(waiting); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("barrier over a join that failed its cleanup = %v", err)
	}
}

func TestJoinerRejectsPolicyMismatchAndUnsupportedExecution(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	shape := flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.LatestFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
	binding := NewJoiner("in", in, flow.ZipFanIn, "out", out)
	if err := binding.Validate(shape); !errors.Is(err, ErrBinding) {
		t.Fatalf("policy mismatch = %v", err)
	}
	latest := NewJoiner("in", in, flow.LatestFanIn, "out", out)
	joiner := &sumJoiner{operatorBase: operatorBase{shape}, output: out}
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	sinkLink, _ := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	_, latestOwner := testOwner("join")
	if _, _, err := latest.OpenJoiner(joiner, 2, queue.Limit{Items: 1}, 0, sinkLink, latestOwner); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported policy = %v", err)
	}
}
