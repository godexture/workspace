package acme

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type writerPlan struct {
	shape  flow.Shape
	header []byte
}

func writerComponent() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("values", Values())},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
	spec := plugin.Spec[configuration, writerPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[writerPlan, stream.Descriptor], error) {
			input, ok := inputs.One("values")
			if !ok {
				return plugin.Compiled[writerPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("values", plugin.ConditionNeed[stream.Descriptor]("acme.value")),
				}}, nil
			}
			resolver, ok := metadata.ResolverOf(ctx)
			if !ok {
				return plugin.Compiled[writerPlan, stream.Descriptor]{}, errors.New("ACME writer requires metadata resolver")
			}
			label, err := resolver.Marshal(ctx.Context(), LabelCarrier(), "acme/label", input.Metadata())
			if err != nil {
				return plugin.Compiled[writerPlan, stream.Descriptor]{}, err
			}
			labelBytes := label.AppendTo(nil)
			header := make([]byte, 5+len(labelBytes))
			copy(header[0:4], "ACM1")
			header[4] = byte(len(labelBytes))
			copy(header[5:], labelBytes)
			output, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
			if err != nil {
				return plugin.Compiled[writerPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[writerPlan, stream.Descriptor]{
				Plan: writerPlan{shape: shape.Clone(), header: header}, Outputs: flow.NewDescriptors(flow.Describe("writes", output.WithMetadata(input.Metadata()))),
				Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "acme-mux"}},
				Resources: resource.Request{Memory: resource.Bytes(len(header) + 1)},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan writerPlan) (flow.Operator, error) {
			if len(plan.header) <= 5 || ctx.Buffers() == nil {
				return nil, errors.New("ACME writer requires a prepared header and payload grant")
			}
			return &writerOperator{shape: plan.shape.Clone(), buffers: ctx.Buffers(), header: append([]byte(nil), plan.header...)}, nil
		},
	}
	return plugin.NewComponent[writerID](plugin.Descriptor{DisplayName: "ACME writer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("values", Values(), "writes", access.Writes()),
		mediaformat.Write(Container(), access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)
}

type writerOperator struct {
	shape   flow.Shape
	buffers *buffer.Allocator
	header  []byte
	wrote   bool
}

func (o *writerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*writerOperator) Close() error        { return nil }

func (o *writerOperator) Process(ctx context.Context, input flow.Input[Value], output flow.Emitter[access.Write]) error {
	if !input.Valid() {
		return errors.New("ACME writer received invalid value")
	}
	size := 1
	includeHeader := !o.wrote
	if includeHeader {
		size += len(o.header)
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: size}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		destination := storage.Bytes()
		offset := 0
		if includeHeader {
			offset = copy(destination, o.header)
		}
		destination[offset] = input.Value().Number
		return nil
	}); err != nil {
		return err
	}
	handle, err := lease.Commit()
	if err != nil {
		return err
	}
	write, err := access.Append(handle)
	if err != nil {
		handle.Release()
		return err
	}
	item := flow.NewInput(write, access.Writes())
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	o.wrote = true
	input.Drop()
	return nil
}

func (o *writerOperator) Flush(context.Context, flow.Emitter[access.Write]) error {
	if !o.wrote {
		return ErrMalformed
	}
	return nil
}
