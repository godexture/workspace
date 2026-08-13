package file

import (
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

const blockSize = 64 * 1024

type (
	configID      struct{}
	configuration struct{}
	sourcePlan    struct{ shape flow.Shape }
	sinkPlan      struct{ shape flow.Shape }
)

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{} }).Version("1").Build()
}

func sourceShape() flow.Shape {
	return flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
}

func sinkShape() flow.Shape {
	return flow.NewShape([]flow.Port{flow.In("writes", access.Writes())}, nil)
}

func sourceComponent() plugin.Component {
	shape := sourceShape()
	spec := plugin.Spec[configuration, sourcePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(plugin.CompileContext, configuration, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[sourcePlan, stream.Descriptor], error) {
			descriptor, err := stream.NewDescriptor("file", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
			if err != nil {
				return plugin.Compiled[sourcePlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[sourcePlan, stream.Descriptor]{
				Plan:      sourcePlan{shape: shape.Clone()},
				Outputs:   flow.NewDescriptors(flow.Describe("bytes", descriptor)),
				Resources: resource.Request{Memory: blockSize},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan sourcePlan) (flow.Operator, error) {
			opening, ok := plugin.Boundary[access.Opening](ctx)
			if !ok {
				return nil, errors.New("file source requires a prepared Access opening")
			}
			return openSource(plan.shape, opening, ctx.Buffers())
		},
	}
	return plugin.NewComponent[sourceID](plugin.Descriptor{DisplayName: "File source"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithReader("bytes", access.Bytes()),
		access.Source("file", sourceCapabilities(), acquireSource),
	)
}

func sinkComponent() plugin.Component {
	return sinkComponentWith(
		plugin.Descriptor{DisplayName: "File sink"},
		access.Sink("file", sinkCapabilities(), access.AtomicReplace, acquireSink, access.WithEquivalence(equivalent)),
	)
}

func sinkComponentWith(descriptor plugin.Descriptor, traits ...plugin.ComponentOption) plugin.Component {
	shape := sinkShape()
	spec := plugin.Spec[configuration, sinkPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(_ plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[sinkPlan, stream.Descriptor], error) {
			input, ok := inputs.One("writes")
			if !ok {
				return plugin.Compiled[sinkPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("writes", plugin.ConditionNeed[stream.Descriptor]("file.input"))},
				}, nil
			}
			if input.TimeBase() != access.CarrierTimeBase() || input.Properties().Len() != 0 {
				desired, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
				if err != nil {
					return plugin.Compiled[sinkPlan, stream.Descriptor]{}, err
				}
				desired = desired.WithMetadata(input.Metadata())
				return plugin.Compiled[sinkPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.DescriptorNeed("file.carrier", desired))},
				}, nil
			}
			return plugin.Compiled[sinkPlan, stream.Descriptor]{
				Plan:      sinkPlan{shape: shape.Clone()},
				Resources: resource.Request{Memory: blockSize},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan sinkPlan) (flow.Operator, error) {
			opening, ok := plugin.Boundary[access.Opening](ctx)
			if !ok {
				return nil, errors.New("file sink requires a prepared Access opening")
			}
			return openSink(plan.shape, opening)
		},
	}
	options := []plugin.ComponentOption{
		plugin.WithSpec(spec),
		plugin.WithWriter("writes", access.Writes()),
	}
	options = append(options, traits...)
	return plugin.NewComponent[sinkID](descriptor, configurationSchema(), options...)
}
