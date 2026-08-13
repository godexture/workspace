// The testkit-owned components a scenario is built from: the fixture source
// and sink, the rejecting operator, and their Access sessions.
package testkit

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type fixturePluginID struct{}
type fixtureSourceID struct{}
type fixtureSinkID struct{}
type fixtureRejectID struct{}
type fixtureConfigID struct{}

type fixtureConfig struct{}

type fixturePlan struct{ shape flow.Shape }

type lifecycleState struct {
	sourceOpen  atomic.Int32
	sourceClose atomic.Int32
	sinkOpen    atomic.Int32
	sinkClose   atomic.Int32
	eof         atomic.Int32
}

func fixtureDefinition[I, O any](kind runnerKind, subject Subject[I, O], input *Fixture[I], output recorder[O], state *lifecycleState, settings scenarioSettings) plugin.Definition {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", subject.input.schema)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", subject.output.schema)}, nil)
	sourceSpec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sourceShape),
		Compile: func(plugin.CompileContext, fixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:    fixturePlan{shape: sourceShape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", input.descriptor)),
			}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			state.sourceOpen.Add(1)
			return &fixtureReader[I]{shape: sourceShape, input: input, state: state}, nil
		},
	}
	sinkSpec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sinkShape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("in"); !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.output")),
				}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{Plan: fixturePlan{shape: sinkShape.Clone()}}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			state.sinkOpen.Add(1)
			return &fixtureWriter[O]{shape: sinkShape, output: output, state: state}, nil
		},
	}
	components := []plugin.Component{}
	if settings.reject != nil {
		components = append(components, rejectComponent(subject, settings.reject))
	}
	sourceOptions := []plugin.ComponentOption{plugin.WithSpec(sourceSpec), plugin.WithReader("out", subject.input.schema)}
	sinkOptions := []plugin.ComponentOption{plugin.WithSpec(sinkSpec), plugin.WithWriter("in", subject.output.schema)}
	if kind == formatRunner {
		if component, ok := componentOf(subject.set, subject.identity); ok {
			if _, read := mediaformat.ReadOf(component); read {
				capabilities, _ := access.NewCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)
				sourceOptions = append(sourceOptions, access.Source("testkit", capabilities, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
					payload, err := carrierBytes(input.values)
					if err != nil {
						return nil, err
					}
					return &readSession{data: payload, caps: capabilities}, nil
				}))
			}
			if _, write := mediaformat.WriteOf(component); write {
				capabilities, _ := access.NewCapabilities(access.SequentialWrite, access.RandomWrite)
				sinkOptions = append(sinkOptions, access.Sink("testkit", capabilities, access.LiveNoCommit, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
					return &writeSession{caps: capabilities}, nil
				}))
			}
		}
	}
	source := plugin.NewComponent[fixtureSourceID](plugin.Descriptor{DisplayName: "testkit source"}, schema, sourceOptions...)
	sink := plugin.NewComponent[fixtureSinkID](plugin.Descriptor{DisplayName: "testkit sink"}, schema, sinkOptions...)
	components = append(components, source, sink)
	return plugin.Define[fixturePluginID](plugin.Descriptor{DisplayName: "testkit fixtures", Version: "1"}, components...)
}

// rejectComponent is a pass-through processor that refuses one item. Host
// fuses adjacent linear processors into a single island, so the subject sees
// its Emit fail inside its own Process call.
func rejectComponent[I, O any](subject Subject[I, O], reject *rejection) plugin.Component {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", subject.output.schema)}, []flow.Port{flow.Out("out", subject.output.schema)})
	spec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](shape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.reject")),
				}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:    fixturePlan{shape: shape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return &rejectOperator[O]{shape: shape.Clone(), reject: reject}, nil
		},
	}
	return plugin.NewComponent[fixtureRejectID](plugin.Descriptor{DisplayName: "testkit rejection"}, schema,
		plugin.WithSpec(spec), plugin.WithProcessor("in", subject.output.schema, "out", subject.output.schema))
}

type rejectOperator[T any] struct {
	shape  flow.Shape
	reject *rejection
}

func (o *rejectOperator[T]) Ports() flow.Shape { return o.shape.Clone() }
func (*rejectOperator[T]) Close() error        { return nil }

func (o *rejectOperator[T]) Process(ctx context.Context, input *flow.Item[T], output flow.Emitter[T]) error {
	if err := o.reject.accept(); err != nil {
		return err
	}
	return output.Emit(ctx, input)
}

func (*rejectOperator[T]) Flush(context.Context, flow.Emitter[T]) error { return nil }

type fixtureReader[T any] struct {
	shape  flow.Shape
	input  *Fixture[T]
	state  *lifecycleState
	mu     sync.Mutex
	index  int
	closed bool
}

func (r *fixtureReader[T]) Ports() flow.Shape { return r.shape.Clone() }

func (r *fixtureReader[T]) Read(ctx context.Context, into *flow.Item[T]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.index >= len(r.input.values) {
		r.state.eof.Add(1)
		return io.EOF
	}
	*into = r.input.emit(r.index)
	r.index++
	return nil
}

func (r *fixtureReader[T]) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.state.sourceClose.Add(1)
	}
	return nil
}

type fixtureWriter[T any] struct {
	shape  flow.Shape
	output recorder[T]
	state  *lifecycleState
	closed bool
}

func (w *fixtureWriter[T]) Ports() flow.Shape { return w.shape.Clone() }

func (w *fixtureWriter[T]) Write(_ context.Context, input *flow.Item[T]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("testkit sink received an invalid input")
	}
	w.output.accept(input.Value())
	return nil
}

func (w *fixtureWriter[T]) Close() error {
	if !w.closed {
		w.closed = true
		w.state.sinkClose.Add(1)
	}
	return nil
}

type readSession struct {
	data   []byte
	offset int64
	closed atomic.Bool
	caps   access.Capabilities
}

func (s *readSession) Capabilities() access.Capabilities { return s.caps }
func (s *readSession) Close() error {
	s.closed.Store(true)
	return nil
}
func (s *readSession) Read(ctx context.Context, destination []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count, err := s.ReadAt(ctx, destination, s.offset)
	s.offset += int64(count)
	return count, err
}
func (s *readSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	if count < len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *readSession) Size(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.closed.Load() {
		return 0, errors.New("testkit read session is closed")
	}
	return int64(len(s.data)), nil
}

type writeSession struct{ caps access.Capabilities }

func (s *writeSession) Capabilities() access.Capabilities { return s.caps }
func (*writeSession) Close() error                        { return nil }
func (*writeSession) Write(ctx context.Context, value []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(value), nil
}
func (*writeSession) WriteAt(ctx context.Context, value []byte, _ int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(value), nil
}
