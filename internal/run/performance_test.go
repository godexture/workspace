package run

import (
	"context"
	"fmt"
	"io"
	"testing"

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
		return fmt.Errorf("runtime report: %#v", report)
	}
	wantSum := int64(benchmarkLinearItems*(benchmarkLinearItems-1)/2 + benchmarkLinearItems*2)
	if writer.count != benchmarkLinearItems || writer.sum != wantSum {
		return fmt.Errorf("result = count %d sum %d, want count %d sum %d", writer.count, writer.sum, benchmarkLinearItems, wantSum)
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
