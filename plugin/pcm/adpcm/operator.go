package adpcm

import (
	"context"
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"

	imaadpcm "github.com/godexture/godec/plugin/pcm/internal/adpcm/ima"
	msadpcm "github.com/godexture/godec/plugin/pcm/internal/adpcm/ms"
)

type operatorBase struct {
	shape   flow.Shape
	buffers *buffer.Allocator
}

func (o operatorBase) Ports() flow.Shape { return o.shape.Clone() }
func (operatorBase) Close() error        { return nil }

type parserOperator struct {
	operatorBase
	out        flow.Item[packet.Packet]
	parameters param.Parameters
}

func (o *parserOperator) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("ADPCM parser received an unowned chunk")
	}
	align := int(o.parameters.BlockAlign)
	length := input.Value().Bytes().Len()
	if align == 0 || length%align != 0 {
		return ErrPartialBlock
	}
	duration := timing.SomeDuration(timing.NewDuration(int64(length / align * int(o.parameters.SamplesPerBlock))))
	transferErr := flow.Transfer(input, &o.out, output, func(chunk packet.Chunk) (packet.Packet, error) {
		pts, dts := chunk.PTS(), chunk.DTS()
		if !dts.Valid() {
			if value, ok := pts.Get(); ok {
				dts = timing.SomeDTS(timing.NewDTS(value.Int64()))
			}
		}
		return packet.NewPacket(chunk.Sequence(), pts, dts, duration, chunk.Detach()).WithSideData(chunk.SideData()), nil
	})
	defer o.out.Drop()
	if transferErr != nil {
		return transferErr
	}
	return output.Emit(ctx, &o.out)
}

func (*parserOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

// expand is the block decoder of one variant, chosen once at Open so a block
// never dispatches on the layout again.
type expand func(planes [][]int16, block []byte, parameters param.Parameters) error

type decoderOperator struct {
	operatorBase
	out        flow.Item[audio.Frame[int16]]
	expand     expand
	parameters param.Parameters
	channels   int
	planes     []buffer.PlaneSpec
	windows    [][]int16
	block      []byte
}

func newDecoderOperator(plan componentPlan, buffers *buffer.Allocator) *decoderOperator {
	move := expand(msadpcm.Decode)
	if plan.variant == IMA {
		move = imaadpcm.Decode
	}
	return &decoderOperator{
		operatorBase: operatorBase{shape: plan.shape.Clone(), buffers: buffers},
		expand:       move,
		parameters:   plan.parameters,
		channels:     plan.channels,
		planes:       make([]buffer.PlaneSpec, plan.channels),
		windows:      make([][]int16, plan.channels),
		block:        make([]byte, plan.parameters.BlockAlign),
	}
}

func (o *decoderOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[audio.Frame[int16]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("ADPCM decoder received an unowned packet")
	}
	value := input.Value()
	encoded := value.Bytes()
	align := int(o.parameters.BlockAlign)
	if align == 0 || encoded.Len()%align != 0 {
		return ErrPartialBlock
	}
	blocks := encoded.Len() / align
	samples := blocks * int(o.parameters.SamplesPerBlock)
	for index := range o.planes {
		o.planes[index].Size = samples * 2
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: o.planes})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error { return o.fill(storage, encoded, samples, blocks) }); err != nil {
		return err
	}
	storage, err := lease.Commit()
	if err != nil {
		return err
	}
	frame, err := audio.NewFrame[int16](value.PTS(), samples, storage)
	if err != nil {
		storage.Release()
		return err
	}
	output.Own(&o.out, frame.WithSideData(value.SideData()))
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

// fill expands the packet one block at a time. Each block restates the
// predictor state it needs, so a block is drained through the operator scratch
// and expanded straight into the window of the planes it belongs to.
func (o *decoderOperator) fill(storage buffer.Mutable, encoded buffer.Bytes, samples, blocks int) error {
	planes := make([][]int16, o.channels)
	for channel := range planes {
		plane, err := storage.Plane(channel)
		if err != nil {
			return err
		}
		if planes[channel], err = audio.Plane[int16](plane, samples); err != nil {
			return err
		}
	}
	perBlock := int(o.parameters.SamplesPerBlock)
	align := int(o.parameters.BlockAlign)
	for index := range blocks {
		view, err := encoded.Slice(index*align, align)
		if err != nil {
			return err
		}
		if view.CopyTo(o.block) != align {
			return ErrPartialBlock
		}
		for channel := range o.channels {
			o.windows[channel] = planes[channel][index*perBlock : (index+1)*perBlock]
		}
		if err := o.expand(o.windows, o.block, o.parameters); err != nil {
			return err
		}
	}
	return nil
}

func (*decoderOperator) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error { return nil }
