package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type (
	lifecyclePluginID struct{}
	lifecycleConfigID struct{}
	lifecyclePassID   struct{}
	lifecycleFormatID struct{}
	lifecycleConfig   struct{}
	lifecyclePlan     struct{ shape flow.Shape }
)

var errLifecycle = errors.New("injected file lifecycle failure")

type lifecycleOperator struct{ shape flow.Shape }

func (o lifecycleOperator) Ports() flow.Shape { return o.shape.Clone() }
func (lifecycleOperator) Close() error        { return nil }

func (o lifecycleOperator) Process(ctx context.Context, input flow.Input[buffer.Handle], output flow.Emitter[buffer.Handle]) error {
	if !input.Valid() {
		return errors.New("lifecycle pass received invalid bytes")
	}
	item := flow.NewInput(input.Take().Value(), access.Bytes())
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (lifecycleOperator) Flush(context.Context, flow.Emitter[buffer.Handle]) error { return nil }

type failingSinkOperator struct {
	*sinkOperator
	phase host.Phase
}

func (o *failingSinkOperator) PrepareCommit(ctx context.Context) error {
	if o.phase == host.PrepareCommitPhase {
		return errLifecycle
	}
	return o.sinkOperator.PrepareCommit(ctx)
}

func (o *failingSinkOperator) Commit(ctx context.Context) error {
	if o.phase == host.CommitPhase {
		return errLifecycle
	}
	return o.sinkOperator.Commit(ctx)
}

func TestHostFailurePhasesAbortFileTransaction(t *testing.T) {
	tests := []struct {
		name      string
		openFails bool
		sinkPhase host.Phase
		wantPhase host.Phase
	}{
		{name: "open", openFails: true, wantPhase: host.OpenPhase},
		{name: "prepare commit", sinkPhase: host.PrepareCommitPhase, wantPhase: host.PrepareCommitPhase},
		{name: "commit", sinkPhase: host.CommitPhase, wantPhase: host.CommitPhase},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.bin")
			outputPath := filepath.Join(directory, "output.bin")
			if err := os.WriteFile(inputPath, []byte("new payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outputPath, []byte("old payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			instance, request := lifecycleFixture(t, inputPath, outputPath, test.openFails, test.sinkPhase)
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if matches := temporaryFiles(t, outputPath); len(matches) != 1 {
				t.Fatalf("temporary files after Prepare = %v", matches)
			}
			result, err := prepared.Run(context.Background())
			if err == nil || result.Primary == nil || result.Primary.Phase != test.wantPhase || test.wantPhase != host.OpenPhase && !errors.Is(err, errLifecycle) {
				t.Fatalf("failure result = %#v, err = %v", result, err)
			}
			assertFile(t, outputPath, []byte("old payload"))
			if matches := temporaryFiles(t, outputPath); len(matches) != 0 {
				t.Fatalf("temporary files after %s failure = %v", test.wantPhase, matches)
			}
		})
	}
}

func TestCanceledRunClosesPreparedFileTransaction(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.bin")
	outputPath := filepath.Join(directory, "output.bin")
	if err := os.WriteFile(inputPath, []byte("new payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("old payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	instance, request := lifecycleFixture(t, inputPath, outputPath, false, "")
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := prepared.Run(ctx)
	if err == nil || !errors.Is(err, context.Canceled) || result.Primary == nil {
		t.Fatalf("canceled result = %#v, err = %v", result, err)
	}
	assertFile(t, outputPath, []byte("old payload"))
	if matches := temporaryFiles(t, outputPath); len(matches) != 0 {
		t.Fatalf("temporary files after cancellation = %v", matches)
	}
}

func lifecycleFixture(t *testing.T, inputPath, outputPath string, openFails bool, sinkPhase host.Phase) (*host.Host, job.Job) {
	t.Helper()
	pass := lifecycleComponent(openFails)
	set := plugin.NewSet(Plugin()).Add(plugin.Define[lifecyclePluginID](plugin.Descriptor{DisplayName: "File lifecycle fixture", Version: "1"}, pass))
	if sinkPhase != "" {
		set = set.Override(SinkIdentity(), lifecycleSinkComponent(sinkPhase))
	}
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph([]job.Node{job.NewNode("pass", pass.Identity(), config.NewPatch())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(fileReference(t, inputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(fileReference(t, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		t.Fatal(err)
	}
	return instance, request
}

func lifecycleComponent(openFails bool) plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("in", access.Bytes())},
		[]flow.Port{flow.Out("out", access.Bytes())},
	)
	schema := lifecycleSchema()
	spec := plugin.Spec[lifecycleConfig, lifecyclePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[lifecycleConfig](shape),
		Compile: func(_ plugin.CompileContext, _ lifecycleConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[lifecyclePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[lifecyclePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("lifecycle.input"))}}, nil
			}
			return plugin.Compiled[lifecyclePlan, stream.Descriptor]{
				Plan:    lifecyclePlan{shape: shape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			}, nil
		},
		Open: func(plugin.OpenContext, lifecyclePlan) (flow.Operator, error) {
			if openFails {
				return nil, errLifecycle
			}
			return &lifecycleOperator{shape: shape.Clone()}, nil
		},
	}
	formatValue, err := mediaformat.Define[lifecycleFormatID](nil)
	if err != nil {
		panic(err)
	}
	return plugin.NewComponent[lifecyclePassID](plugin.Descriptor{DisplayName: "Carrier pass"}, schema,
		plugin.WithSpec(spec),
		plugin.WithProcessor("in", access.Bytes(), "out", access.Bytes()),
		mediaformat.Read(formatValue, access.AnyOf(access.SequentialRead)),
		mediaformat.Write(formatValue, access.AnyOf(access.SequentialWrite)),
	)
}

func lifecycleSinkComponent(phase host.Phase) plugin.Component {
	shape := sinkShape()
	spec := plugin.Spec[configuration, sinkPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(_ plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[sinkPlan, stream.Descriptor], error) {
			if _, ok := inputs.One("bytes"); !ok {
				return plugin.Compiled[sinkPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("file.input"))}}, nil
			}
			return plugin.Compiled[sinkPlan, stream.Descriptor]{Plan: sinkPlan{shape: shape.Clone()}}, nil
		},
		Open: func(ctx plugin.OpenContext, plan sinkPlan) (flow.Operator, error) {
			opening, ok := plugin.Boundary[access.Opening](ctx)
			if !ok {
				return nil, errors.New("file sink requires a prepared Access opening")
			}
			opened, err := openSink(plan.shape, opening)
			if err != nil {
				return nil, err
			}
			return &failingSinkOperator{sinkOperator: opened.(*sinkOperator), phase: phase}, nil
		},
	}
	return plugin.NewComponent[sinkID](plugin.Descriptor{DisplayName: "Failing file sink"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithWriter("bytes", access.Bytes()),
		access.Sink("file", sinkCapabilities(), access.AtomicReplace, acquireSink),
	)
}

func lifecycleSchema() config.Schema[lifecycleConfig] {
	return config.Struct[lifecycleConfigID](func() lifecycleConfig { return lifecycleConfig{} }).Version("1").Build()
}
