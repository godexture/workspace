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
	*into = flow.NewItem(r.values[r.index], r.typ)
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
	item := flow.NewItem(owned{value: input.Value().value + 10}, p.output)
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
	task, err := sourceBinding.OpenSource(source, processorLink)
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	fanout, err := sourceBinding.Fanout([]Link{leftLink, rightLink})
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[owned](fanout)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewItem(owned{value: 9}, typ)
	if err := target.Emit(context.Background(), &input); !errors.Is(err, want) {
		t.Fatalf("fan-out error = %v", err)
	}
	input.Drop()
	if owners.forks.Load() != 1 || owners.drops.Load() != 2 {
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
	item := flow.NewItem(owned{value: 4}, typ)
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
	w.value = samples[0]
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
	fanout, err := NewSource("out", typ).Fanout([]Link{leftLink, rightLink})
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[audio.Frame[int16]](fanout)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(frame, typ)
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
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if err := sourceTask.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if got := sink.Values(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("buffered values = %v", got)
	}
	if owners.forks.Load() != 0 || owners.drops.Load() != 3 {
		t.Fatalf("buffer ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

type intOperator struct {
	operatorBase
	out flow.Item[int]
}

func (p *intOperator) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	p.out = flow.NewItemWithTraits(input.Value()+1, nil, nil)
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
		cell.Set(1, in)
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
	if _, err := NewSource("out", typ).OpenSource(&intReader{operatorBase: operatorBase{operator.Ports()}, typ: typ}, otherLink); !errors.Is(err, ErrLink) {
		t.Fatalf("payload link error = %v", err)
	}
	_ = other
}

type intReader struct {
	operatorBase
	typ schema.Type[int]
}

func (r *intReader) Read(_ context.Context, into *flow.Item[int]) error {
	*into = flow.NewItem(1, r.typ)
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
	item := flow.NewItem(owned{value: left.value + right.value}, j.output)
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
	inputs, task, err := binding.OpenJoiner(joiner, 2, queue.Limit{Items: 2}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := deliveryOf[owned](inputs[0])
	right, _ := deliveryOf[owned](inputs[1])
	result := make(chan error, 1)
	go func() { result <- task.Run(context.Background()) }()
	for _, pair := range [][2]int{{1, 10}, {2, 20}} {
		leftItem := flow.NewItem(owned{value: pair[0]}, in)
		if err := left.Emit(context.Background(), &leftItem); err != nil {
			t.Fatal(err)
		}
		leftItem.Drop()
		rightItem := flow.NewItem(owned{value: pair[1]}, in)
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
}

func TestZipJoinerEnforcesTimestampWatermark(t *testing.T) {
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
	inputs, task, err := NewJoiner("in", in, flow.ZipFanIn, "out", out).OpenJoiner(joiner, 2, queue.Limit{Items: 2, Time: 5}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- task.Run(context.Background()) }()
	left, _ := deliveryOf[owned](inputs[0])
	right, _ := deliveryOf[owned](inputs[1])
	leftItem := flow.NewItem(owned{value: 1}, in)
	defer leftItem.Drop()
	if err := left.Emit(context.Background(), &leftItem); err != nil {
		t.Fatal(err)
	}
	rightItem := flow.NewItem(owned{value: 10}, in)
	defer rightItem.Drop()
	if err := right.Emit(context.Background(), &rightItem); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrWatermark) {
		t.Fatalf("watermark error = %v", err)
	}
	task.Discard()
	if inputOwners.drops.Load() != 2 {
		t.Fatalf("watermark input drops = %d", inputOwners.drops.Load())
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
	if _, _, err := latest.OpenJoiner(joiner, 2, queue.Limit{Items: 1}, sinkLink); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported policy = %v", err)
	}
}
