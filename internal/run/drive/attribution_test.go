package drive

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

type attributionReader struct {
	typ  schema.Type[int]
	read bool
}

type attributionSchemaID struct{}

func (r *attributionReader) Ports() flow.Shape {
	return flow.NewShape(nil, []flow.Port{flow.Out("out", r.typ)})
}
func (*attributionReader) Close() error { return nil }

func (r *attributionReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	into.Set(1)
	return nil
}

type attributionProcessor struct {
	shape   flow.Shape
	typ     schema.Type[int]
	failure error
	panics  bool
}

func (p *attributionProcessor) Ports() flow.Shape { return p.shape.Clone() }
func (*attributionProcessor) Close() error        { return nil }

func (p *attributionProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	value := input.Value()
	input.Drop()
	if p.panics {
		panic(p.failure)
	}
	if p.failure != nil {
		return p.failure
	}
	item := flow.NewItem(value, p.typ, &testDomain)
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (*attributionProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type attributionWriter struct {
	shape   flow.Shape
	failure error
	panics  bool
}

func (w *attributionWriter) Ports() flow.Shape { return w.shape.Clone() }
func (*attributionWriter) Close() error        { return nil }

func (w *attributionWriter) Write(context.Context, *flow.Item[int]) error {
	if w.panics {
		panic(w.failure)
	}
	return w.failure
}

// TestLinearDeliveryFailureIsAttributedToTheCallbackNode fixes the node
// boundary for a fused source -> processor -> sink chain. The task domain is
// the source's, but a component callback failure is the plugin node's event;
// returning that event as a Cause must not make the outer source span rewrite
// its attribution.
func TestLinearDeliveryFailureIsAttributedToTheCallbackNode(t *testing.T) {
	testDeliveryFailureAttribution(t, false)
}

// TestBufferedDeliveryFailureIsAttributedToTheCallbackNode exercises the same
// callback boundary through a drain task. Its domain is the buffer's, while
// the failure still belongs to the processor or sink node that raised it.
func TestBufferedDeliveryFailureIsAttributedToTheCallbackNode(t *testing.T) {
	testDeliveryFailureAttribution(t, true)
}

func testDeliveryFailureAttribution(t *testing.T, buffered bool) {
	t.Helper()
	for _, test := range []struct {
		name string
		node string
		kind journal.Kind
		make func(flow.Shape, schema.Type[int]) (flow.Operator, flow.Operator)
	}{
		{
			name: "processor error",
			node: "processor",
			kind: journal.WorkError,
			make: func(processorShape flow.Shape, typ schema.Type[int]) (flow.Operator, flow.Operator) {
				return &attributionProcessor{
					shape:   processorShape,
					typ:     typ,
					failure: errors.New("processor failure"),
				}, &attributionWriter{shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)}
			},
		},
		{
			name: "processor panic",
			node: "processor",
			kind: journal.WorkPanic,
			make: func(processorShape flow.Shape, typ schema.Type[int]) (flow.Operator, flow.Operator) {
				return &attributionProcessor{
					shape:   processorShape,
					typ:     typ,
					failure: errors.New("processor panic"),
					panics:  true,
				}, &attributionWriter{shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)}
			},
		},
		{
			name: "sink error",
			node: "sink",
			kind: journal.WorkError,
			make: func(processorShape flow.Shape, typ schema.Type[int]) (flow.Operator, flow.Operator) {
				return &attributionProcessor{shape: processorShape, typ: typ}, &attributionWriter{
					shape:   flow.NewShape([]flow.Port{flow.In("in", typ)}, nil),
					failure: errors.New("sink failure"),
				}
			},
		},
		{
			name: "sink panic",
			node: "sink",
			kind: journal.WorkPanic,
			make: func(processorShape flow.Shape, typ schema.Type[int]) (flow.Operator, flow.Operator) {
				return &attributionProcessor{shape: processorShape, typ: typ}, &attributionWriter{
					shape:   flow.NewShape([]flow.Port{flow.In("in", typ)}, nil),
					failure: errors.New("sink panic"),
					panics:  true,
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			typ := schema.Define[attributionSchemaID](schema.Traits[int]{})
			processorShape := flow.NewShape(
				[]flow.Port{flow.In("in", typ)},
				[]flow.Port{flow.Out("out", typ)},
			)
			processor, writer := test.make(processorShape, typ)
			if err := NewProcessor("in", typ, "out", typ).ValidateOperator(processor); err != nil {
				t.Fatal(err)
			}
			if err := NewSink("in", typ).ValidateOperator(writer); err != nil {
				t.Fatal(err)
			}
			sinkLink, err := NewSink("in", typ).OpenSinkAt(writer, "sink")
			if err != nil {
				t.Fatal(err)
			}
			processorLink, err := NewProcessor("in", typ, "out", typ).PrependAt(processor, sinkLink, "processor")
			if err != nil {
				t.Fatal(err)
			}
			ledger := journal.NewLedger()
			sourceOwner := ledger.Domain("source", "source")
			var sourceTarget Link = processorLink
			var bufferTask Task
			if buffered {
				sourceTarget, bufferTask, err = NewSource("out", typ).Buffer(queue.Limit{Items: 2}, processorLink, ledger.Domain("buffer", "buffer"))
				if err != nil {
					t.Fatal(err)
				}
			}
			sourceTask, err := NewSource("out", typ).OpenSource(&attributionReader{typ: typ}, sourceTarget, sourceOwner)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			bufferDone := make(chan error, 1)
			if buffered {
				go func() { bufferDone <- perform(ctx, bufferTask) }()
			}
			runErr := perform(ctx, sourceTask)
			if buffered {
				if runErr != nil {
					t.Fatalf("source task failed before the buffered callback: %v", runErr)
				}
			} else if runErr == nil {
				t.Fatal("direct source task completed without the callback failure")
			}
			if err := sourceTask.Finish(ctx); err != nil {
				t.Fatalf("source finish = %v", err)
			}
			if buffered {
				if err := <-bufferDone; err == nil {
					t.Fatal("buffer task completed without the callback failure")
				}
			}
			events := ledger.Events()
			if len(events) != 1 {
				t.Fatalf("events = %#v, want exactly one callback failure", events)
			}
			failure := events[0]
			if failure.Node != test.node || failure.Kind != test.kind || failure.Operation != journal.Run {
				t.Fatalf("failure = %#v, want node %q kind %v operation Run", failure, test.node, test.kind)
			}
			if failure.Node == "source" || failure.Node == "buffer" {
				t.Fatalf("failure was attributed to a home node: %#v", failure)
			}
		})
	}
}

func TestJoinedCancellationAndIndependentFailureIsNotAnEcho(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	stop := errors.New("external stop")
	independent := errors.New("callback failed while stopping")
	cancel(stop)

	ledger := journal.NewLedger()
	domain := ledger.Domain("task", "home")
	site := domain.At("sink")
	if cause := domain.Perform(journal.Run, func(*journal.Span) error {
		return performDelivery(ctx, site, func() error {
			return errors.Join(stop, independent)
		})
	}); cause == nil {
		t.Fatal("joined callback failure completed without a cause")
	}

	events := ledger.Events()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want both joined occurrences", events)
	}
	var sawStop, sawIndependent bool
	for _, event := range events {
		sawStop = sawStop || errors.Is(event.Err, stop)
		sawIndependent = sawIndependent || errors.Is(event.Err, independent)
	}
	if !sawStop || !sawIndependent {
		t.Fatalf("events = %#v, want cancellation and independent callback failure", events)
	}
}
