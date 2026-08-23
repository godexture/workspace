package g711

import (
	"context"
	"errors"
	"unsafe"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

// blockWords sizes the scratch a codec drains its input through. It is a
// uint64 array so the block is aligned for the samples it is retyped as.
const blockWords = 512

type operatorBase struct {
	shape   flow.Shape
	buffers *buffer.Allocator
}

func (o operatorBase) Ports() flow.Shape { return o.shape.Clone() }
func (operatorBase) Close() error        { return nil }

type parserOperator struct {
	operatorBase
	out      flow.Item[packet.Packet]
	channels int
}

func (o *parserOperator) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("companded parser received an unowned chunk")
	}
	if o.channels == 0 || input.Value().Bytes().Len()%o.channels != 0 {
		return ErrPartialSample
	}
	duration := timing.SomeDuration(timing.NewDuration(int64(input.Value().Bytes().Len() / o.channels)))
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

func openCodec(plan componentPlan, buffers *buffer.Allocator) (flow.Operator, error) {
	base := operatorBase{shape: plan.shape.Clone(), buffers: buffers}
	switch plan.operation {
	case decoderOperation:
		return &decoderOperator{
			operatorBase: base,
			channels:     plan.channels,
			expand:       plan.law.expansion(),
			planes:       make([]buffer.PlaneSpec, plan.channels),
			windows:      make([][]int16, plan.channels),
		}, nil
	case encoderOperation:
		return &encoderOperator{
			operatorBase: base,
			channels:     plan.channels,
			compand:      plan.law.companding(),
		}, nil
	default:
		return nil, errors.New("unknown companded operation")
	}
}

type decoderOperator struct {
	operatorBase
	out      flow.Item[audio.Frame[int16]]
	channels int
	expand   *[256]uint16
	planes   []buffer.PlaneSpec
	windows  [][]int16
	scratch  [blockWords]uint64
}

func (o *decoderOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[audio.Frame[int16]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("companded decoder received an unowned packet")
	}
	value := input.Value()
	encoded := value.Bytes()
	if o.channels == 0 || encoded.Len()%o.channels != 0 {
		return ErrPartialSample
	}
	samples := encoded.Len() / o.channels
	for index := range o.planes {
		o.planes[index].Size = samples * 2
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: o.planes})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error { return o.fill(storage, encoded, samples) }); err != nil {
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

func (o *decoderOperator) fill(storage buffer.Mutable, encoded buffer.Bytes, samples int) error {
	for channel := range o.channels {
		plane, err := storage.Plane(channel)
		if err != nil {
			return err
		}
		if o.windows[channel], err = audio.Plane[int16](plane, samples); err != nil {
			return err
		}
	}
	block := scratchBytes(o.scratch[:])
	limit := len(block) - len(block)%o.channels
	if limit == 0 {
		return ErrPartialSample
	}
	return encoded.Blocks(block[:limit], func(part []byte, offset int) error {
		count := len(part) / o.channels
		first := offset / o.channels
		for channel, window := range o.windows {
			index := channel
			for position := range window[first : first+count] {
				window[first+position] = int16(o.expand[part[index]])
				index += o.channels
			}
		}
		return nil
	})
}

func (*decoderOperator) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error { return nil }

type encoderOperator struct {
	operatorBase
	out      flow.Item[packet.Packet]
	channels int
	compand  *[65536]byte
	sequence uint64
	scratch  [blockWords]uint64
}

func (o *encoderOperator) Process(ctx context.Context, input *flow.Item[audio.Frame[int16]], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("companded encoder received an unowned frame")
	}
	frame := input.Value()
	if len(frame.Planes().Layout().Planes) != o.channels {
		return ErrPlaneCount
	}
	samples := frame.Samples()
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: samples * o.channels}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error { return o.fill(storage.Bytes(), frame, samples) }); err != nil {
		return err
	}
	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	value := packet.NewPacket(o.sequence, frame.PTS(), dtsFromPTS(frame.PTS()),
		timing.SomeDuration(timing.NewDuration(int64(samples))), payload).WithSideData(frame.SideData())
	output.Own(&o.out, value)
	defer o.out.Drop()
	if err := output.Emit(ctx, &o.out); err != nil {
		return err
	}
	o.sequence++
	return nil
}

func (o *encoderOperator) fill(target []byte, frame audio.Frame[int16], samples int) error {
	block := scratchBytes(o.scratch[:])
	limit := len(block) - len(block)%2
	if limit == 0 || len(target) < samples*o.channels {
		return ErrPartialSample
	}
	for channel := range o.channels {
		plane, err := frame.Plane(channel)
		if err != nil {
			return err
		}
		occupied, err := plane.Slice(0, samples*2)
		if err != nil {
			return err
		}
		err = occupied.Blocks(block[:limit], func(part []byte, offset int) error {
			window, err := audio.Plane[int16](part, len(part)/2)
			if err != nil {
				return err
			}
			index := offset/2*o.channels + channel
			for _, value := range window {
				target[index] = o.compand[uint16(value)]
				index += o.channels
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (*encoderOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

func dtsFromPTS(pts timing.OptionalPTS) timing.OptionalDTS {
	value, ok := pts.Get()
	if !ok {
		return timing.UnknownDTS()
	}
	return timing.SomeDTS(timing.NewDTS(value.Int64()))
}

func scratchBytes(words []uint64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
}
