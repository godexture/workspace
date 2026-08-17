package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type (
	cliDiagnosticPluginID struct{}
	cliDiagnosticSourceID struct{}
	cliDiagnosticSinkID   struct{}
	cliDiagnosticConfigID struct{}
	cliDiagnosticConfig   struct{}
	cliDiagnosticPlan     struct{ shape flow.Shape }
)

var errCLIDiagnosticFailure = errors.New("independent CLI diagnostic fixture failure")

func TestRenderResultKeepsHostFailureWhenPluginEmitsDiagnostic(t *testing.T) {
	definition := cliDiagnosticPlugin()
	instance, err := host.New(host.Plugins(plugin.NewSet(definition)))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", plugin.IdentityOf[cliDiagnosticSourceID](), config.NewPatch()),
			job.NewNode("sink", plugin.IdentityOf[cliDiagnosticSinkID](), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "bytes"), job.At("sink", "bytes"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr == nil || result.Succeeded() {
		t.Fatalf("fixture unexpectedly succeeded: result=%#v err=%v", result, runErr)
	}
	if len(result.Diagnostics) < 2 {
		t.Fatalf("real Host result diagnostics = %#v, want plugin and host failure items", result.Diagnostics)
	}
	var stdout, stderr strings.Builder
	if err := renderResult(&stdout, &stderr, result, runErr, false); err != nil {
		t.Fatal(err)
	}
	output := stderr.String()
	if !strings.Contains(output, "cli.warning") || !strings.Contains(output, "host.run") || !strings.Contains(output, errCLIDiagnosticFailure.Error()) {
		t.Fatalf("rendered diagnostics omitted real failure evidence: %s", output)
	}
	if strings.Count(output, errCLIDiagnosticFailure.Error()) != 1 {
		t.Fatalf("real failure was duplicated after diagnostic rendering: %s", output)
	}
}

func cliDiagnosticPlugin() plugin.Definition {
	shapeSource := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	shapeSink := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, nil)
	schema := config.Struct[cliDiagnosticConfigID](func() cliDiagnosticConfig { return cliDiagnosticConfig{} }).Version("1").Build()
	source := plugin.NewComponent[cliDiagnosticSourceID](
		plugin.Descriptor{DisplayName: "CLI diagnostic source"}, schema,
		plugin.WithSpec(plugin.Spec[cliDiagnosticConfig, cliDiagnosticPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[cliDiagnosticConfig](shapeSource),
			Compile: func(_ plugin.CompileContext, _ cliDiagnosticConfig, _ flow.Descriptors[stream.Descriptor]) (plugin.Compiled[cliDiagnosticPlan, stream.Descriptor], error) {
				descriptor, err := stream.NewDescriptor("diagnostic", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
				if err != nil {
					return plugin.Compiled[cliDiagnosticPlan, stream.Descriptor]{}, err
				}
				return plugin.Compiled[cliDiagnosticPlan, stream.Descriptor]{
					Plan:      cliDiagnosticPlan{shape: shapeSource.Clone()},
					Outputs:   flow.NewDescriptors(flow.Describe("bytes", descriptor)),
					Resources: resource.Request{Memory: 1},
				}, nil
			},
			Open: func(ctx plugin.OpenContext, plan cliDiagnosticPlan) (flow.Operator, error) {
				return &cliDiagnosticSource{shape: plan.shape, buffers: ctx.Buffers()}, nil
			},
		}),
		plugin.WithReader("bytes", access.Bytes()),
	)
	sink := plugin.NewComponent[cliDiagnosticSinkID](
		plugin.Descriptor{DisplayName: "CLI diagnostic sink"}, schema,
		plugin.WithSpec(plugin.Spec[cliDiagnosticConfig, cliDiagnosticPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[cliDiagnosticConfig](shapeSink),
			Compile: func(_ plugin.CompileContext, _ cliDiagnosticConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[cliDiagnosticPlan, stream.Descriptor], error) {
				if _, ok := inputs.One("bytes"); !ok {
					return plugin.Compiled[cliDiagnosticPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("cli.diagnostic.input"))}}, nil
				}
				return plugin.Compiled[cliDiagnosticPlan, stream.Descriptor]{Plan: cliDiagnosticPlan{shape: shapeSink.Clone()}, Resources: resource.Request{Memory: 1}}, nil
			},
			Open: func(ctx plugin.OpenContext, plan cliDiagnosticPlan) (flow.Operator, error) {
				return &cliDiagnosticSink{shape: plan.shape, diagnostics: ctx.Diagnostics()}, nil
			},
		}),
		plugin.WithWriter("bytes", access.Bytes()),
	)
	return plugin.Define[cliDiagnosticPluginID](plugin.Descriptor{DisplayName: "CLI diagnostic fixture", Version: "1", Build: plugin.BuildModePureGo}, source, sink)
}

type cliDiagnosticSource struct {
	shape   flow.Shape
	buffers *buffer.Allocator
	emitted bool
}

func (o *cliDiagnosticSource) Ports() flow.Shape { return o.shape.Clone() }
func (*cliDiagnosticSource) Close() error        { return nil }

func (o *cliDiagnosticSource) Read(_ context.Context, into *flow.Item[buffer.Handle]) error {
	if o.emitted {
		return io.EOF
	}
	if into == nil || !into.Bound() {
		return errors.New("diagnostic source received an unbound output")
	}
	if o.buffers == nil {
		return errors.New("diagnostic source has no buffer grant")
	}
	handle, err := o.buffers.FromBytes([]byte{1}, 1)
	if err != nil {
		return err
	}
	into.Set(handle)
	o.emitted = true
	return nil
}

type cliDiagnosticSink struct {
	shape       flow.Shape
	diagnostics diagnostic.Sink
}

func (o *cliDiagnosticSink) Ports() flow.Shape { return o.shape.Clone() }
func (*cliDiagnosticSink) Close() error        { return nil }

func (o *cliDiagnosticSink) Write(_ context.Context, input *flow.Item[buffer.Handle]) error {
	defer input.Drop()
	if o.diagnostics != nil {
		o.diagnostics.Emit(diagnostic.NewItem("cli.warning", diagnostic.WarningSeverity, diagnostic.Path{}, "fixture warning", nil))
	}
	return errCLIDiagnosticFailure
}
