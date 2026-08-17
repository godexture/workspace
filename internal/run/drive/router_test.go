package drive

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type routeOperator struct {
	operatorBase
	out     flow.Item[owned]
	route   int
	invalid bool
	flushed int
}

func (r *routeOperator) Process(ctx context.Context, input *flow.Item[owned], outputs flow.RoutedEmitter[owned]) error {
	defer input.Drop()
	route := r.route
	if r.invalid {
		route = -1
	}
	emitter, ok := outputs.Route(route)
	if !ok {
		return nil
	}
	emitter.Own(&r.out, input.Value())
	defer r.out.Drop()
	return emitter.Emit(ctx, &r.out)
}

func (r *routeOperator) Flush(context.Context, flow.RoutedEmitter[owned]) error {
	r.flushed++
	return nil
}

type intRouter struct {
	operatorBase
	out flow.Item[int]
}

func (r *intRouter) Process(ctx context.Context, input *flow.Item[int], outputs flow.RoutedEmitter[int]) error {
	defer input.Drop()
	route, ok := outputs.Route(0)
	if !ok {
		panic("router route 0 is unavailable")
	}
	route.Own(&r.out, input.Value()+1)
	defer r.out.Drop()
	return route.Emit(ctx, &r.out)
}

func (*intRouter) Flush(context.Context, flow.RoutedEmitter[int]) error { return nil }

type multiRouteOperator struct {
	operatorBase
	first  flow.Item[owned]
	second flow.Item[owned]
}

func (r *multiRouteOperator) Process(ctx context.Context, input *flow.Item[owned], outputs flow.RoutedEmitter[owned]) error {
	defer input.Drop()
	first, ok := outputs.Route(0)
	if !ok {
		return errors.New("router route 0 is unavailable")
	}
	second, ok := outputs.Route(1)
	if !ok {
		return errors.New("router route 1 is unavailable")
	}
	first.Own(&r.first, owned{value: 0})
	defer r.first.Drop()
	if err := first.Emit(ctx, &r.first); err != nil {
		return err
	}
	second.Own(&r.second, owned{value: 1})
	defer r.second.Drop()
	return second.Emit(ctx, &r.second)
}

func (*multiRouteOperator) Flush(context.Context, flow.RoutedEmitter[owned]) error { return nil }

type retainingWriter struct {
	operatorBase
	values []int
}

func (w *retainingWriter) Write(_ context.Context, input *flow.Item[owned]) error {
	w.values = append(w.values, input.Value().value)
	return nil
}

func TestRouterSelectsRouteWithoutForkingAndRefusesInvalidOrdinals(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	routerShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ)},
		[]flow.Port{flow.Out("out", typ, flow.Many())},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	router := &routeOperator{operatorBase: operatorBase{routerShape}, route: 1}
	left := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	right := &recordingWriter{operatorBase: operatorBase{sinkShape}}

	binding := NewRouter("in", typ, "out", typ)
	if err := binding.Validate(routerShape); err != nil {
		t.Fatal(err)
	}
	leftLink, err := NewSink("in", typ).OpenSink(left)
	if err != nil {
		t.Fatal(err)
	}
	rightLink, err := NewSink("in", typ).OpenSink(right)
	if err != nil {
		t.Fatal(err)
	}
	routerLink, err := binding.OpenRouterAt(router, []Link{leftLink, rightLink}, "router")
	if err != nil {
		t.Fatal(err)
	}
	source := &sliceReader{operatorBase: operatorBase{flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})}, typ: typ, values: []owned{{value: 7}}}
	ledger, owner := testOwner("source")
	task, err := NewSource("out", typ).OpenSource(source, routerLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := task.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := left.Values(); len(got) != 0 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.Values(); len(got) != 1 || got[0] != 7 {
		t.Fatalf("right values = %v", got)
	}
	if owners.forks.Load() != 0 {
		t.Fatalf("router forked %d values", owners.forks.Load())
	}
	if router.flushed != 1 {
		t.Fatalf("router flush count = %d", router.flushed)
	}

	router.invalid = true
	input := flow.NewItem(owned{value: 9}, typ, &testDomain)
	target, err := deliveryOf[owned](routerLink)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Perform(journal.Run, func(*journal.Span) error { return target.Emit(context.Background(), &input) }); err != nil {
		t.Fatal(err)
	}
	input.Drop()
	if got := right.Values(); len(got) != 1 {
		t.Fatalf("invalid route emitted %v", got)
	}
	if events := ledger.Events(); len(events) != 0 {
		t.Fatalf("ledger = %#v", events)
	}
}

func TestRouterFanoutOwnsOnlyItsSelectedRoute(t *testing.T) {
	owners := &ownership{}
	typ := ownedSchema[driveOutputID](owners)
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	right := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	leftLink, _ := NewSink("in", typ).OpenSink(left)
	rightLink, _ := NewSink("in", typ).OpenSink(right)
	binding := NewRouter("in", typ, "out", typ)
	route, err := binding.Fanout([]Link{leftLink, rightLink}, "router")
	if err != nil {
		t.Fatal(err)
	}
	routerLink, err := binding.OpenRouterAt(&routeOperator{operatorBase: operatorBase{routerShape}}, []Link{route}, "router")
	if err != nil {
		t.Fatal(err)
	}
	source := &sliceReader{operatorBase: operatorBase{flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})}, typ: typ, values: []owned{{value: 3}}}
	_, owner := testOwner("source")
	task, err := NewSource("out", typ).OpenSource(source, routerLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if got := left.Values(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.Values(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("right values = %v", got)
	}
	if owners.forks.Load() != 1 {
		t.Fatalf("forks = %d, want one fanout fork", owners.forks.Load())
	}
}

func TestRouterRouteItemsKeepTheirReporter(t *testing.T) {
	typ := schema.Define[driveOutputID](schema.Traits[owned]{
		Drop: func(value owned) {
			if value.value == 1 {
				panic("route 1 release")
			}
		},
	})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	left := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	right := &retainingWriter{operatorBase: operatorBase{sinkShape}}
	leftLink, err := NewSink("in", typ).OpenSinkAt(left, "left")
	if err != nil {
		t.Fatal(err)
	}
	rightLink, err := NewSink("in", typ).OpenSinkAt(right, "right")
	if err != nil {
		t.Fatal(err)
	}
	routerLink, err := NewRouter("in", typ, "out", typ).OpenRouterAt(&multiRouteOperator{operatorBase: operatorBase{routerShape}}, []Link{leftLink, rightLink}, "router")
	if err != nil {
		t.Fatal(err)
	}
	source := &sliceReader{operatorBase: operatorBase{flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})}, typ: typ, values: []owned{{value: 9}}}
	ledger, owner := testOwner("source")
	task, err := NewSource("out", typ).OpenSource(source, routerLink, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := perform(context.Background(), task); err == nil {
		t.Fatal("route 1 release failure did not stop the task")
	}
	if got := left.Values(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.values; len(got) != 1 || got[0] != 1 {
		t.Fatalf("right values = %v", got)
	}
	events := ledger.Events()
	if len(events) != 1 || !events[0].Kind.Cleanup() || events[0].Node != "right" {
		t.Fatalf("route 1 cleanup = %#v", events)
	}
}

func TestRouterHopAllocatesZero(t *testing.T) {
	type inputID struct{}
	type outputID struct{}
	in := schema.Define[inputID](schema.Traits[int]{})
	out := schema.Define[outputID](schema.Traits[int]{})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", in)}, []flow.Port{flow.Out("out", out, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	next, err := NewSink("in", out).OpenSink(&intWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewRouter("in", in, "out", out).OpenRouter(&intRouter{operatorBase: operatorBase{routerShape}}, []Link{next})
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
		t.Fatalf("router hop allocations = %v", allocations)
	}
}

func TestRouterBindingRequiresOneInputAndManyOutput(t *testing.T) {
	typ := ownedSchema[driveOutputID](&ownership{})
	binding := NewRouter("in", typ, "out", typ)
	valid := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	if err := binding.Validate(valid); err != nil {
		t.Fatal(err)
	}
	for _, shape := range []flow.Shape{
		flow.NewShape([]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))}, []flow.Port{flow.Out("out", typ, flow.Many())}),
		flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)}),
	} {
		if err := binding.Validate(shape); !errors.Is(err, ErrBinding) {
			t.Fatalf("shape %v error = %v", shape, err)
		}
	}
}
