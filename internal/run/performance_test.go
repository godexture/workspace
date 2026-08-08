package run

import (
	"context"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
	legacynode "github.com/godexture/godec/core/node"
	legacy "github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

const benchmarkLinearItems = 32 << 10

type benchmarkSchemaID struct{}

var benchmarkSchema = schema.Define[benchmarkSchemaID](schema.Traits[int]{})

type benchmarkOperator struct{ shape flow.Shape }

func (o benchmarkOperator) Ports() flow.Shape { return o.shape.Clone() }
func (benchmarkOperator) Close() error        { return nil }

type benchmarkReader struct {
	benchmarkOperator
	remaining int
}

func (r *benchmarkReader) Read(context.Context) (flow.Input[int], error) {
	if r.remaining == 0 {
		return flow.Input[int]{}, io.EOF
	}
	value := benchmarkLinearItems - r.remaining
	r.remaining--
	return flow.NewInput(value, benchmarkSchema), nil
}

type benchmarkProcessor struct {
	benchmarkOperator
	delta int
}

func (p *benchmarkProcessor) Process(ctx context.Context, input flow.Input[int], output flow.Emitter[int]) error {
	return output.Emit(ctx, flow.NewInput(input.Value()+p.delta, benchmarkSchema))
}

func (*benchmarkProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type benchmarkWriter struct {
	benchmarkOperator
	count int
	sum   int64
}

func (w *benchmarkWriter) Write(_ context.Context, input flow.Input[int]) error {
	w.count++
	w.sum += int64(input.Value())
	return nil
}

func benchmarkTemplate() (Template, flow.Shape, flow.Shape, flow.Shape) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", benchmarkSchema)})
	processorShape := flow.NewShape(
		[]flow.Port{flow.In("in", benchmarkSchema)},
		[]flow.Port{flow.Out("out", benchmarkSchema)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", benchmarkSchema)}, nil)
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", benchmarkSchema)},
			{ID: "first", Shape: processorShape, Execution: drive.NewProcessor("in", benchmarkSchema, "out", benchmarkSchema)},
			{ID: "second", Shape: processorShape, Execution: drive.NewProcessor("in", benchmarkSchema, "out", benchmarkSchema)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", benchmarkSchema)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("first", "in")),
			job.Connect(job.At("first", "out"), job.At("second", "in")),
			job.Connect(job.At("second", "out"), job.At("sink", "in")),
		},
	)
	if err != nil {
		panic(err)
	}
	return template, sourceShape, processorShape, sinkShape
}

func runCurrentLinear(template Template, sourceShape, processorShape, sinkShape flow.Shape) error {
	writer := &benchmarkWriter{benchmarkOperator: benchmarkOperator{shape: sinkShape}}
	execution, err := template.Build([]flow.Operator{
		&benchmarkReader{benchmarkOperator: benchmarkOperator{shape: sourceShape}, remaining: benchmarkLinearItems},
		&benchmarkProcessor{benchmarkOperator: benchmarkOperator{shape: processorShape}, delta: 1},
		&benchmarkProcessor{benchmarkOperator: benchmarkOperator{shape: processorShape}, delta: 1},
		writer,
	})
	if err != nil {
		return err
	}
	report := execution.Run(context.Background())
	if !report.Complete() || len(report.Failures) != 0 {
		return fmt.Errorf("current runtime report: %#v", report)
	}
	return validateBenchmarkResult("current", writer.count, writer.sum)
}

type legacySource struct {
	out   map[string]*legacynode.OutPort[int]
	items int
}

func newLegacySource(items int) *legacySource {
	return &legacySource{
		out:   map[string]*legacynode.OutPort[int]{"out": legacynode.NewOutPort[int]("out", media.StreamInfo{})},
		items: items,
	}
}

func (s *legacySource) OutputPorts() map[string]*legacynode.OutPort[int] { return s.out }
func (*legacySource) Close() error                                       { return nil }
func (s *legacySource) Start(ctx context.Context) error {
	defer s.out["out"].Edge().Close()
	for value := range s.items {
		if err := s.out["out"].Push(ctx, value); err != nil {
			return err
		}
	}
	return nil
}

type legacyProcessor struct {
	in    map[string]*legacynode.InPort[int]
	out   map[string]*legacynode.OutPort[int]
	delta int
}

func newLegacyProcessor(delta int) *legacyProcessor {
	return &legacyProcessor{
		in:    map[string]*legacynode.InPort[int]{"in": legacynode.NewInPort[int]("in")},
		out:   map[string]*legacynode.OutPort[int]{"out": legacynode.NewOutPort[int]("out", media.StreamInfo{})},
		delta: delta,
	}
}

func (p *legacyProcessor) InputPorts() map[string]*legacynode.InPort[int] { return p.in }
func (p *legacyProcessor) OutputPorts() map[string]*legacynode.OutPort[int] {
	return p.out
}
func (*legacyProcessor) Close() error { return nil }
func (p *legacyProcessor) Start(ctx context.Context) error {
	defer p.out["out"].Edge().Close()
	for {
		value, err := p.in["in"].Pull(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := p.out["out"].Push(ctx, value+p.delta); err != nil {
			return err
		}
	}
}

type legacySink struct {
	in    map[string]*legacynode.InPort[int]
	count int
	sum   int64
}

func newLegacySink() *legacySink {
	return &legacySink{in: map[string]*legacynode.InPort[int]{"in": legacynode.NewInPort[int]("in")}}
}

func (s *legacySink) InputPorts() map[string]*legacynode.InPort[int] { return s.in }
func (*legacySink) Close() error                                     { return nil }
func (s *legacySink) Start(ctx context.Context) error {
	for {
		value, err := s.in["in"].Pull(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.count++
		s.sum += int64(value)
	}
}

func runLegacyLinear() error {
	source := newLegacySource(benchmarkLinearItems)
	first := newLegacyProcessor(1)
	second := newLegacyProcessor(1)
	sink := newLegacySink()
	if err := legacy.LinkWithBufferSize(source, "out", first, "in", 4); err != nil {
		return err
	}
	if err := legacy.LinkWithBufferSize(first, "out", second, "in", 4); err != nil {
		return err
	}
	if err := legacy.LinkWithBufferSize(second, "out", sink, "in", 4); err != nil {
		return err
	}
	pipeline, err := legacy.New(source, first, second, sink)
	if err != nil {
		return err
	}
	if err := pipeline.Run(context.Background()); err != nil {
		return err
	}
	return validateBenchmarkResult("legacy", sink.count, sink.sum)
}

func validateBenchmarkResult(runtime string, count int, sum int64) error {
	wantSum := int64(benchmarkLinearItems*(benchmarkLinearItems-1)/2 + benchmarkLinearItems*2)
	if count != benchmarkLinearItems || sum != wantSum {
		return fmt.Errorf("%s result = count %d sum %d, want count %d sum %d", runtime, count, sum, benchmarkLinearItems, wantSum)
	}
	return nil
}

func BenchmarkLinear(b *testing.B) {
	template, sourceShape, processorShape, sinkShape := benchmarkTemplate()
	b.ReportAllocs()
	b.SetBytes(benchmarkLinearItems * 8)
	for b.Loop() {
		if err := runCurrentLinear(template, sourceShape, processorShape, sinkShape); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRuntimePaired(b *testing.B) {
	template, sourceShape, processorShape, sinkShape := benchmarkTemplate()
	current := func() error { return runCurrentLinear(template, sourceShape, processorShape, sinkShape) }
	legacyAllocs := testing.AllocsPerRun(5, func() {
		if err := runLegacyLinear(); err != nil {
			panic(err)
		}
	})
	currentAllocs := testing.AllocsPerRun(5, func() {
		if err := current(); err != nil {
			panic(err)
		}
	})
	b.ReportMetric(legacyAllocs, "legacy-allocs/run")
	b.ReportMetric(currentAllocs, "current-allocs/run")
	b.SetBytes(benchmarkLinearItems * 8)
	ratios := make([]float64, 0, b.N)
	b.ResetTimer()
	for sample := 0; sample < b.N; sample++ {
		var legacyDuration, currentDuration time.Duration
		if sample%2 == 0 {
			legacyDuration = measureBenchmarkRun(b, runLegacyLinear)
			currentDuration = measureBenchmarkRun(b, current)
		} else {
			currentDuration = measureBenchmarkRun(b, current)
			legacyDuration = measureBenchmarkRun(b, runLegacyLinear)
		}
		ratios = append(ratios, float64(currentDuration)/float64(legacyDuration))
	}
	b.StopTimer()
	b.ReportMetric(medianBenchmarkRatio(ratios), "current/legacy")
}

func measureBenchmarkRun(b *testing.B, run func() error) time.Duration {
	b.Helper()
	started := time.Now()
	if err := run(); err != nil {
		b.Fatal(err)
	}
	return time.Since(started)
}

func medianBenchmarkRatio(values []float64) float64 {
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}
