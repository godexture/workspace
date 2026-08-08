package drive

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
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

func (r *sliceReader) Read(context.Context) (flow.Input[owned], error) {
	if r.index == len(r.values) {
		return flow.Input[owned]{}, io.EOF
	}
	value := flow.NewInput(r.values[r.index], r.typ)
	r.index++
	return value, nil
}

type mapProcessor struct {
	operatorBase
	input  schema.Type[owned]
	output schema.Type[owned]
	flush  atomic.Int32
}

func (p *mapProcessor) Process(ctx context.Context, input flow.Input[owned], output flow.Emitter[owned]) error {
	item := flow.NewInput(owned{value: input.Value().value + 10}, p.output)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
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

func (w *recordingWriter) Write(_ context.Context, input flow.Input[owned]) error {
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
	task, err := sourceBinding.OpenSource(source, processorLink)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
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
	fanout, err := sourceBinding.Fanout([]Link{leftLink, rightLink})
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[owned](fanout)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewInput(owned{value: 9}, typ)
	if err := target.Emit(context.Background(), input); !errors.Is(err, want) {
		t.Fatalf("fan-out error = %v", err)
	}
	input.Drop()
	if owners.forks.Load() != 2 || owners.drops.Load() != 3 {
		t.Fatalf("fan-out ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
	if got := left.Values(); len(got) != 1 || got[0] != 9 || len(right.Values()) != 0 {
		t.Fatalf("fan-out values = left %v right %v", got, right.Values())
	}
}

func TestOneOutputFanoutIsLinearMove(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{shape}}
	link, _ := NewSink("in", typ).OpenSink(sink)
	linear, err := NewSource("out", typ).Fanout([]Link{link})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := deliveryOf[owned](linear)
	if err := target.Emit(context.Background(), flow.NewInput(owned{value: 4}, typ)); err != nil {
		t.Fatal(err)
	}
	if owners.forks.Load() != 0 || owners.drops.Load() != 1 {
		t.Fatalf("one-output ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

func TestBufferedLinkDrainsInOrderAndClosesDownstream(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	sinkLink, _ := NewSink("in", typ).OpenSink(sink)
	buffered, bufferTask, err := NewSource("out", typ).Buffer(queue.Limit{Items: 2, Bytes: 2, Time: 10}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	source := &sliceReader{operatorBase: operatorBase{sourceShape}, typ: typ, values: []owned{{1}, {2}, {3}}}
	sourceTask, err := NewSource("out", typ).OpenSource(source, buffered)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- bufferTask.Run(ctx) }()
	go func() { results <- sourceTask.Run(ctx) }()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := sink.Values(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("buffered values = %v", got)
	}
	if owners.forks.Load() != 0 || owners.drops.Load() != 3 {
		t.Fatalf("buffer ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

type intOperator struct{ operatorBase }

func (p intOperator) Process(ctx context.Context, input flow.Input[int], output flow.Emitter[int]) error {
	return output.Emit(ctx, flow.NewInputWithTraits(input.Value()+1, nil, nil))
}

func (intOperator) Flush(context.Context, flow.Emitter[int]) error { return nil }

type intWriter struct {
	operatorBase
	value int
}

func (w *intWriter) Write(_ context.Context, input flow.Input[int]) error {
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
	link, err := NewProcessor("in", in, "out", out).Prepend(intOperator{operatorBase{processorShape}}, next)
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[int](link)
	if err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(1000, func() {
		if err := target.Emit(context.Background(), flow.NewInputWithTraits(1, nil, nil)); err != nil {
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
	if _, err := NewSource("out", typ).OpenSource(&intReader{operatorBase: operatorBase{operator.Ports()}, typ: typ}, otherLink); !errors.Is(err, ErrLink) {
		t.Fatalf("payload link error = %v", err)
	}
	_ = other
}

type intReader struct {
	operatorBase
	typ schema.Type[int]
}

func (r *intReader) Read(context.Context) (flow.Input[int], error) {
	return flow.NewInput(1, r.typ), nil
}
