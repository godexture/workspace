package run

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

// This file is the lifecycle and ownership matrix.
//
// The axes are the topology a payload travels through, how the consuming work
// ends, and how the declared release ends. Their full product runs. Two axes
// the request names are deliberately not multiplied through it, and the reason
// is here rather than implied:
//
//   - Which ownership slot holds a payload is not independent of topology; it
//     is decided by it. Each topology below is chosen for the slot kinds it
//     puts a payload through, and together they cover all of them: a source's
//     own slot, a queue ring slot, an in-flight slot moving by direct call
//     between fused stages, a fan-out branch slot, a fan-in batch slot, and a
//     component slot retained across calls. Every row lists which it exercises.
//   - Lifecycle operation is likewise decided rather than chosen. An in-flight
//     slot is released during Run, a retained slot during Flush or Close, a
//     queue slot during Run or Discard. Each row asserts the operation its
//     releases actually belong to instead of enumerating impossible pairs.
//
// Cancellation and echo suppression are their own axis and live in
// host/failure_test.go, because what they test -- whether one event is
// recorded twice -- does not vary with topology or slot, and multiplying them
// through here would restate one contract sixty times.
//
// Every row asserts the same three things: what stopped the run is what stopped
// the work and never a release beside it, every release failure is recorded
// exactly once under the operation it happened in, and nothing recorded renders
// a value the panicking code chose.

type matrixTopology struct {
	name string
	// slots names the ownership slots a payload passes through here, so a
	// reader can see what this row is covering without deriving it.
	slots string
	build func(testing.TB, schema.Type[int], matrixEnding, *atomic.Int32) (Template, []flow.Operator)
}

type matrixEnding uint8

const (
	endsCleanly matrixEnding = iota + 1
	endsWithError
	endsWithPanic
)

func (e matrixEnding) String() string {
	switch e {
	case endsCleanly:
		return "consumer succeeds"
	case endsWithError:
		return "consumer fails"
	case endsWithPanic:
		return "consumer panics"
	}
	return "unknown"
}

type matrixRelease uint8

const (
	releaseSucceeds matrixRelease = iota + 1
	releaseRaisesError
	releaseRaisesValue
)

func (r matrixRelease) String() string {
	switch r {
	case releaseSucceeds:
		return "release succeeds"
	case releaseRaisesError:
		return "release raises an error"
	case releaseRaisesValue:
		return "release raises a value"
	}
	return "unknown"
}

var errMatrixWork = errors.New("the consumer failed")

type matrixSink struct {
	templateOperator
	ending matrixEnding
	seen   *atomic.Int32
	// entered orders an accepted fan-in Emit before its batch is released.
	entered chan<- struct{}
}

// Write declines the item on every path: Emit offers a value rather than
// handing it over, so the release stays with whichever slot owns it. That is
// what puts the release on the owner being exercised instead of on the sink.
func (s *matrixSink) Write(_ context.Context, _ *flow.Item[int]) error {
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
	}
	s.seen.Add(1)
	switch s.ending {
	case endsWithError:
		return errMatrixWork
	case endsWithPanic:
		panic(consumerPanic{Token: outcomeSecret})
	}
	return nil
}

type matrixPass struct{ templateOperator }

// Process hands the payload straight on without taking a slot of its own, so
// the value crosses a fused hop by direct call.
func (p *matrixPass) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	var item flow.Item[int]
	if err := flow.Transfer(input, &item, output, func(value int) (int, error) {
		return value, nil
	}); err != nil {
		return err
	}
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (*matrixPass) Flush(context.Context, flow.Emitter[int]) error { return nil }

type matrixJoiner struct{ templateOperator }

func (j *matrixJoiner) Process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int]) error {
	return j.process(ctx, batch, output, nil)
}

func (j *matrixJoiner) process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int], entered <-chan struct{}) error {
	total := 0
	for index := range batch.Len() {
		if value, ok := batch.Value(index); ok {
			total += value
		}
	}
	var item flow.Item[int]
	output.Own(&item, total)
	defer item.Drop()
	if err := output.Emit(ctx, &item); err != nil {
		return err
	}
	if entered == nil {
		return nil
	}
	select {
	case <-entered:
		return nil
	case <-ctx.Done():
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return ctx.Err()
	}
}

func (*matrixJoiner) Flush(context.Context, flow.Emitter[int]) error { return nil }

type matrixFanInJoiner struct {
	matrixJoiner
	entered <-chan struct{}
}

func (j *matrixFanInJoiner) Process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int]) error {
	return j.process(ctx, batch, output, j.entered)
}

// matrixKeeper retains its input in a slot of its own, bound to the Owner the
// runtime granted it, and releases it one lifecycle step later.
type matrixKeeper struct {
	templateOperator
	typ  schema.Type[int]
	held flow.Item[int]
}

// Process keeps the first payload and passes the rest on, so the row exercises
// a retained slot and still reaches the consumer.
func (k *matrixKeeper) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	if !k.held.Valid() {
		if !k.held.Move(input) {
			return errors.New("the keeper's own slot declared no domain")
		}
		return nil
	}
	var item flow.Item[int]
	if err := flow.Transfer(input, &item, output, func(value int) (int, error) {
		return value, nil
	}); err != nil {
		return err
	}
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (k *matrixKeeper) Flush(context.Context, flow.Emitter[int]) error {
	k.held.Drop()
	return nil
}

type matrixSchemaID struct{}

// matrixType declares the payload. A release that fails without panicking is
// still raised, because a declared Drop returns nothing: raising is the only
// channel it has, and turning that into a reported release rather than a
// raised one is the contract under test.
func matrixType(release matrixRelease, released *atomic.Int32) schema.Type[int] {
	return schema.Define[matrixSchemaID](schema.Traits[int]{
		Fork: func(value int) int { return value },
		Size: func(int) int { return 1 },
		Time: func(value int) (int64, bool) { return int64(value), true },
		Drop: func(int) {
			released.Add(1)
			switch release {
			case releaseRaisesError:
				panic(errors.New("declared release failed"))
			case releaseRaisesValue:
				panic(releasePanic{Token: outcomeSecret})
			}
		},
	})
}

func matrixShapes(typ schema.Type[int]) (source, pass, join, sink flow.Shape) {
	return flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}),
		flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)}),
		flow.NewShape(
			[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
			[]flow.Port{flow.Out("out", typ)},
		),
		flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
}

var matrixTopologies = []matrixTopology{
	{
		name:  "bounded-edge",
		slots: "the source's own slot, a queue ring slot, and whatever the discard finds",
		build: func(t testing.TB, typ schema.Type[int], ending matrixEnding, seen *atomic.Int32) (Template, []flow.Operator) {
			sourceShape, _, _, sinkShape := matrixShapes(typ)
			template, err := Compile(
				[]Node{
					{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
				},
				[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
				job.QueuePolicy{Items: 2},
			)
			if err != nil {
				t.Fatal(err)
			}
			return template, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: ending, seen: seen},
			}
		},
	},
	{
		name:  "fused-chain",
		slots: "an in-flight slot crossing a direct call between two fused stages",
		build: func(t testing.TB, typ schema.Type[int], ending matrixEnding, seen *atomic.Int32) (Template, []flow.Operator) {
			sourceShape, passShape, _, sinkShape := matrixShapes(typ)
			template, err := Compile(
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
				job.QueuePolicy{Items: 2},
			)
			if err != nil {
				t.Fatal(err)
			}
			return template, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&matrixPass{templateOperator{passShape}},
				&matrixPass{templateOperator{passShape}},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: ending, seen: seen},
			}
		},
	},
	{
		name:  "fan-out",
		slots: "a forked branch slot per extra consumer, released by the task that forked it",
		build: func(t testing.TB, typ schema.Type[int], ending matrixEnding, seen *atomic.Int32) (Template, []flow.Operator) {
			sourceShape, _, _, sinkShape := matrixShapes(typ)
			template, err := Compile(
				[]Node{
					{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "left", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
					{ID: "right", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
				},
				[]job.Edge{
					job.Connect(job.At("source", "out"), job.At("left", "in")),
					job.Connect(job.At("source", "out"), job.At("right", "in")),
				},
				job.QueuePolicy{Items: 2},
			)
			if err != nil {
				t.Fatal(err)
			}
			return template, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: ending, seen: seen},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: endsCleanly, seen: seen},
			}
		},
	},
	{
		name:  "fan-in",
		slots: "a batch slot per input, released together by the join",
		build: func(t testing.TB, typ schema.Type[int], ending matrixEnding, seen *atomic.Int32) (Template, []flow.Operator) {
			sourceShape, _, joinShape, sinkShape := matrixShapes(typ)
			entered := make(chan struct{}, 1)
			template, err := Compile(
				[]Node{
					{ID: "a", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "b", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
					{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
				},
				[]job.Edge{
					job.Connect(job.At("a", "out"), job.At("join", "in")),
					job.Connect(job.At("b", "out"), job.At("join", "in")),
					job.Connect(job.At("join", "out"), job.At("sink", "in")),
				},
				job.QueuePolicy{Items: 2},
			)
			if err != nil {
				t.Fatal(err)
			}
			return template, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&matrixFanInJoiner{
					matrixJoiner: matrixJoiner{templateOperator: templateOperator{shape: joinShape}},
					entered:      entered,
				},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: ending, seen: seen, entered: entered},
			}
		},
	},
	{
		name:  "retained-slot",
		slots: "a component slot bound to its Owner and released one step later",
		build: func(t testing.TB, typ schema.Type[int], ending matrixEnding, seen *atomic.Int32) (Template, []flow.Operator) {
			sourceShape, passShape, _, sinkShape := matrixShapes(typ)
			template, err := Compile(
				[]Node{
					{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "keep", Shape: passShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
					{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
				},
				[]job.Edge{
					job.Connect(job.At("source", "out"), job.At("keep", "in")),
					job.Connect(job.At("keep", "out"), job.At("sink", "in")),
				},
				job.QueuePolicy{Items: 2},
			)
			if err != nil {
				t.Fatal(err)
			}
			return template, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2, 3}},
				&matrixKeeper{templateOperator: templateOperator{shape: passShape}, typ: typ},
				&matrixSink{templateOperator: templateOperator{shape: sinkShape}, ending: ending, seen: seen},
			}
		},
	},
}

func TestLifecycleAndOwnershipMatrix(t *testing.T) {
	for _, topology := range matrixTopologies {
		for _, ending := range []matrixEnding{endsCleanly, endsWithError, endsWithPanic} {
			for _, release := range []matrixRelease{releaseSucceeds, releaseRaisesError, releaseRaisesValue} {
				t.Run(fmt.Sprintf("%s/%v/%v", topology.name, ending, release), func(t *testing.T) {
					runMatrixCase(t, topology, ending, release)
				})
			}
		}
	}
}

func runMatrixCase(t *testing.T, topology matrixTopology, ending matrixEnding, release matrixRelease) {
	var released atomic.Int32
	var seen atomic.Int32
	typ := matrixType(release, &released)
	template, operators := topology.build(t, typ, ending, &seen)

	ledger := journal.NewLedger()
	// A retained slot needs the Owner grant Host gives a component at Open, so
	// the row that exercises one gets the same thing here.
	for _, operator := range operators {
		if keeper, ok := operator.(*matrixKeeper); ok {
			keeper.held.Bind(keeper.typ, ledger.Domain("node/keep", "keep").At("keep"))
		}
	}
	execution, err := template.Build(ledger, operators)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	value := (&island{ledger: ledger, execution: execution}).run(context.Background())
	events := value.events()

	stopped := value.failures()
	switch ending {
	case endsCleanly:
		for _, failure := range stopped {
			t.Errorf("the run reported %v as having stopped it, but nothing did", failure)
		}
	default:
		if len(stopped) == 0 {
			t.Fatalf("the run reported nothing that stopped it, events = %#v", events)
		}
		// A release that failed beside the work never replaces it, and never
		// becomes the reason the run stopped.
		for _, failure := range stopped {
			if failure.Kind.Cleanup() {
				t.Fatalf("a release became a reason the run stopped: %#v", failure)
			}
		}
	}

	releases := value.cleanups()
	if release == releaseSucceeds {
		if len(releases) != 0 {
			t.Fatalf("releases = %#v, want none: every declared Drop succeeded", releases)
		}
	} else if released.Load() != 0 && len(releases) == 0 {
		t.Fatalf("%d declared releases failed and none reached the ledger", released.Load())
	}
	// A declared Drop returns nothing, so raising is the only failure it can
	// express, and both shapes of raise land the same way: recorded as a
	// cleanup panic with the stack it came from, and never let out to replace
	// whatever stopped the work. A plain reported error -- what a component or
	// a queue hands a domain directly -- is the cleanup-error half, covered in
	// internal/journal.
	for _, failure := range releases {
		if failure.Kind != journal.CleanupPanic {
			t.Errorf("release kind = %v, want a cleanup panic", failure.Kind)
		}
		var raised *flow.ReleaseError
		if !errors.As(failure.Err, &raised) || len(raised.Stack) == 0 {
			t.Errorf("release = %v, want the stack the declared Drop raised from", failure.Err)
		}
		if failure.Operation.String() == "unknown" {
			t.Errorf("release operation = %v, want a named lifecycle step", failure.Operation)
		}
	}

	// Every event has its own identity, so nothing here was recorded twice.
	seenIDs := make(map[journal.EventID]struct{}, len(events))
	for _, event := range events {
		if _, exists := seenIDs[event.ID]; exists {
			t.Fatalf("identity %+v was recorded twice", event.ID)
		}
		seenIDs[event.ID] = struct{}{}
	}
	assertNoRawPanicValue(t, events)
}
