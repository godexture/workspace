package acme

import (
	"context"
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type decoderPlan struct{ shape flow.Shape }

func decoderComponent() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("packets", codec.Packets())},
		[]flow.Port{flow.Out("values", Values())},
	)
	spec := plugin.Spec[configuration, decoderPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[decoderPlan, stream.Descriptor], error) {
			input, ok := inputs.One("packets")
			if !ok {
				return plugin.Compiled[decoderPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("packets", plugin.ConditionNeed[stream.Descriptor]("acme.packet")),
				}}, nil
			}
			output, err := stream.NewDescriptor(input.ID(), Values().Descriptor(), timing.Base{}, property.New())
			if err != nil {
				return plugin.Compiled[decoderPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[decoderPlan, stream.Descriptor]{
				Plan: decoderPlan{shape: shape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("values", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "acme-increment"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan decoderPlan) (flow.Operator, error) {
			return &decoderOperator{shape: plan.shape.Clone()}, nil
		},
	}
	return plugin.NewComponent[decoderID](plugin.Descriptor{DisplayName: "ACME increment decoder"}, configurationSchema(),
		plugin.WithSpec(spec), plugin.WithProcessor("packets", codec.Packets(), "values", Values()))
}

type decoderOperator struct {
	shape flow.Shape
	out   flow.Item[Value]
}

func (o *decoderOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*decoderOperator) Close() error        { return nil }

func (o *decoderOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[Value]) error {
	if !input.Valid() {
		return errors.New("ACME decoder received invalid packet")
	}
	defer input.Drop()
	data := input.Value().Bytes()
	for index := 0; index < data.Len(); index++ {
		value := data.At(index)
		output.Own(&o.out, Value{Number: value + 1})
		err := output.Emit(ctx, &o.out)
		o.out.Drop()
		if err != nil {
			return err
		}
	}
	return nil
}

func (*decoderOperator) Flush(context.Context, flow.Emitter[Value]) error { return nil }
