package drive

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type routedSourceReader struct {
	operatorBase
	read func(context.Context, flow.RoutedEmitter[owned]) error
}

func (r *routedSourceReader) Read(ctx context.Context, outputs flow.RoutedEmitter[owned]) error {
	return r.read(ctx, outputs)
}

func emitRoutedOwned(ctx context.Context, outputs flow.RoutedEmitter[owned], route int, item *flow.Item[owned], value int) error {
	emitter, ok := outputs.Route(route)
	if !ok {
		return nil
	}
	emitter.Own(item, owned{value: value})
	defer item.Drop()
	return emitter.Emit(ctx, item)
}

func TestRoutedSourceBindingRequiresManyOutputAndTypedReader(t *testing.T) {
	typ := ownedSchema[driveOutputID](&ownership{})
	binding := NewRoutedSource("out", typ)
	valid := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	if err := binding.Validate(valid); err != nil {
		t.Fatal(err)
	}
	for _, shape := range []flow.Shape{
		flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}),
		flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())}),
	} {
		if err := binding.Validate(shape); !errors.Is(err, ErrBinding) {
			t.Fatalf("shape %v error = %v", shape, err)
		}
	}
	reader := &routedSourceReader{operatorBase: operatorBase{shape: valid}, read: func(context.Context, flow.RoutedEmitter[owned]) error { return io.EOF }}
	if err := binding.ValidateOperator(reader); err != nil {
		t.Fatal(err)
	}
	if err := binding.ValidateOperator(&sliceReader{operatorBase: operatorBase{shape: valid}, typ: typ}); !errors.Is(err, ErrOperator) {
		t.Fatalf("ordinary reader validation = %v", err)
	}
}

func TestRoutedSourceEmitsMultipleRoutesAndClosesEveryRoute(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	right := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	leftSink, err := NewSink("in", typ).OpenSinkAt(left, "left")
	if err != nil {
		t.Fatal(err)
	}
	rightSink, err := NewSink("in", typ).OpenSinkAt(right, "right")
	if err != nil {
		t.Fatal(err)
	}
	leftProcessor := &mapProcessor{operatorBase: operatorBase{processorShape}, input: typ, output: typ}
	rightProcessor := &mapProcessor{operatorBase: operatorBase{processorShape}, input: typ, output: typ}
	leftRoute, err := NewProcessor("in", typ, "out", typ).PrependAt(leftProcessor, leftSink, "left-process")
	if err != nil {
		t.Fatal(err)
	}
	rightRoute, err := NewProcessor("in", typ, "out", typ).PrependAt(rightProcessor, rightSink, "right-process")
	if err != nil {
		t.Fatal(err)
	}
	step := 0
	first, second := flow.Item[owned]{}, flow.Item[owned]{}
	reader := &routedSourceReader{
		operatorBase: operatorBase{sourceShape},
		read: func(ctx context.Context, outputs flow.RoutedEmitter[owned]) error {
			if step != 0 {
				return io.EOF
			}
			step++
			if err := emitRoutedOwned(ctx, outputs, 1, &second, 7); err != nil {
				return err
			}
			return emitRoutedOwned(ctx, outputs, 0, &first, 3)
		},
	}
	ledger, owner := testOwner("source")
	task, err := NewRoutedSource("out", typ).OpenRoutedSource(reader, []Link{leftRoute, rightRoute}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := task.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := left.Values(); len(got) != 1 || got[0] != 13 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.Values(); len(got) != 1 || got[0] != 17 {
		t.Fatalf("right values = %v", got)
	}
	if leftProcessor.flush.Load() != 1 || rightProcessor.flush.Load() != 1 {
		t.Fatalf("route flushes = left %d right %d", leftProcessor.flush.Load(), rightProcessor.flush.Load())
	}
	if owners.drops.Load() != 4 {
		t.Fatalf("drops = %d, want input and output ownership released once", owners.drops.Load())
	}
	requireNoFailures(t, ledger)
}

func TestRoutedSourceRejectsReadWithoutProgress(t *testing.T) {
	typ := ownedSchema[driveOutputID](&ownership{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	next, err := NewSink("in", typ).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &routedSourceReader{operatorBase: operatorBase{shape}, read: func(context.Context, flow.RoutedEmitter[owned]) error { return nil }}
	ledger, owner := testOwner("source")
	task, err := NewRoutedSource("out", typ).OpenRoutedSource(reader, []Link{next}, owner)
	if err != nil {
		t.Fatal(err)
	}
	cause := perform(context.Background(), task)
	if !errors.Is(cause, ErrInvalidItem) {
		t.Fatalf("no-progress cause = %v", cause)
	}
	if events := failuresOf(ledger); len(events) != 1 || !errors.Is(events[0].Err, ErrInvalidItem) {
		t.Fatalf("no-progress events = %#v", ledger.Events())
	}
}

func TestRoutedSourceRejectsEmitWithEOFAndWrapsEmitWithErrorOnce(t *testing.T) {
	typ := ownedSchema[driveOutputID](&ownership{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	for _, test := range []struct {
		name    string
		failure error
		want    error
	}{
		{name: "eof", failure: io.EOF, want: ErrReadWithItem},
		{name: "error", failure: errors.New("routed read failed"), want: ErrReadWithItem},
	} {
		t.Run(test.name, func(t *testing.T) {
			next, err := NewSink("in", typ).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
			if err != nil {
				t.Fatal(err)
			}
			var item flow.Item[owned]
			reader := &routedSourceReader{
				operatorBase: operatorBase{shape},
				read: func(ctx context.Context, outputs flow.RoutedEmitter[owned]) error {
					if err := emitRoutedOwned(ctx, outputs, 0, &item, 1); err != nil {
						return err
					}
					return test.failure
				},
			}
			ledger, owner := testOwner("source")
			task, err := NewRoutedSource("out", typ).OpenRoutedSource(reader, []Link{next}, owner)
			if err != nil {
				t.Fatal(err)
			}
			cause := perform(context.Background(), task)
			if !errors.Is(cause, test.want) {
				t.Fatalf("cause = %v, want %v", cause, test.want)
			}
			failures := failuresOf(ledger)
			if len(failures) != 1 || !errors.Is(failures[0].Err, ErrReadWithItem) {
				t.Fatalf("events = %#v", ledger.Events())
			}
			if test.failure != io.EOF && !errors.Is(failures[0].Err, test.failure) {
				t.Fatalf("wrapped failure = %v, want %v", failures[0].Err, test.failure)
			}
		})
	}
}

func TestRoutedSourceFanoutOwnsEachValueExactlyOnce(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	right := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	leftLink, _ := NewSink("in", typ).OpenSink(left)
	rightLink, _ := NewSink("in", typ).OpenSink(right)
	route, err := NewRoutedSource("out", typ).Fanout([]Link{leftLink, rightLink}, "source")
	if err != nil {
		t.Fatal(err)
	}
	call := 0
	var item flow.Item[owned]
	reader := &routedSourceReader{
		operatorBase: operatorBase{sourceShape},
		read: func(ctx context.Context, outputs flow.RoutedEmitter[owned]) error {
			if call != 0 {
				return io.EOF
			}
			call++
			return emitRoutedOwned(ctx, outputs, 0, &item, 8)
		},
	}
	_, owner := testOwner("source")
	task, err := NewRoutedSource("out", typ).OpenRoutedSource(reader, []Link{route}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if got := left.Values(); len(got) != 1 || got[0] != 8 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.Values(); len(got) != 1 || got[0] != 8 {
		t.Fatalf("right values = %v", got)
	}
	if owners.forks.Load() != 1 || owners.drops.Load() != 2 {
		t.Fatalf("fanout ownership = forks %d drops %d", owners.forks.Load(), owners.drops.Load())
	}
}

func TestRoutedSourceEmitterDoesNotAllocatePerItem(t *testing.T) {
	type routedID struct{}
	typ := schema.Define[routedID](schema.Traits[int]{})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	next, err := NewSink("in", typ).OpenSink(&intWriter{operatorBase: operatorBase{shape}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[int](next)
	if err != nil {
		t.Fatal(err)
	}
	state := routedSourceState[int]{}
	state.emitters = []routedSourceEmitter[int]{{state: &state, target: target}}
	emitter, ok := state.Route(0)
	if !ok {
		t.Fatal("route 0 is unavailable")
	}
	var item flow.Item[int]
	defer item.Drop()
	allocations := testing.AllocsPerRun(1000, func() {
		emitter.Own(&item, 1)
		if err := emitter.Emit(context.Background(), &item); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("routed source emitter allocations = %v", allocations)
	}
}

func TestRoutedSourcePanicIsAttributedToSource(t *testing.T) {
	typ := ownedSchema[driveOutputID](&ownership{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	next, err := NewSink("in", typ).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &routedSourceReader{operatorBase: operatorBase{shape}, read: func(context.Context, flow.RoutedEmitter[owned]) error { panic("routed reader panic") }}
	ledger, owner := testOwner("source")
	task, err := NewRoutedSource("out", typ).OpenRoutedSource(reader, []Link{next}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if cause := perform(context.Background(), task); cause == nil {
		t.Fatal("panic did not stop routed source")
	}
	var panicErr *journal.PanicError
	events := failuresOf(ledger)
	if len(events) != 1 || !errors.As(events[0].Err, &panicErr) || panicErr.Location != "source" {
		t.Fatalf("panic events = %#v", ledger.Events())
	}
}

var _ flow.RoutedReader[owned] = (*routedSourceReader)(nil)
