package linear

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

var (
	ErrPartialSample     = errors.New("linear PCM payload ends inside a sample frame")
	ErrPlaneCount        = errors.New("linear PCM frame plane count does not match its channel layout")
	ErrDurationMismatch  = errors.New("linear PCM chunk duration does not match payload sample count")
	ErrUnsupportedCoding = errors.New("linear PCM coding is not supported by this component")
)

type operatorBase struct {
	shape   flow.Shape
	buffers *buffer.Allocator
}

func (o operatorBase) Ports() flow.Shape { return o.shape.Clone() }
func (operatorBase) Close() error        { return nil }

// openFraming opens the operations that move payloads without interpreting
// samples. Decoding and encoding are opened by the typed codec components,
// whose frame port fixes the scalar representation.
func openFraming(plan componentPlan, buffers *buffer.Allocator) (flow.Operator, error) {
	base := operatorBase{shape: plan.shape.Clone(), buffers: buffers}
	switch plan.operation {
	case readerOperation:
		return &readerOperator{operatorBase: base, blockBytes: plan.config.wire().BlockBytes(), chunkSamples: plan.config.ChunkSamples}, nil
	case parserOperation:
		return &parserOperator{operatorBase: base, blockBytes: plan.config.wire().BlockBytes()}, nil
	case writerOperation:
		return &writerOperator{operatorBase: base}, nil
	default:
		return nil, errors.New("unknown linear PCM framing operation")
	}
}

type readerOperator struct {
	operatorBase
	out          flow.Item[packet.Chunk]
	blockBytes   int
	chunkSamples int
	sequence     uint64
	sampleOffset int64
}

func (o *readerOperator) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[packet.Chunk]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("raw PCM reader received unowned bytes")
	}
	data := input.Value().Bytes()
	if o.blockBytes == 0 || data.Len()%o.blockBytes != 0 {
		return ErrPartialSample
	}
	chunkBytes := o.chunkSamples * o.blockBytes
	for offset := 0; offset < data.Len(); offset += chunkBytes {
		end := min(offset+chunkBytes, data.Len())
		payload, err := input.Value().Range(offset, end-offset)
		if err != nil {
			return err
		}
		samples := int64((end - offset) / o.blockBytes)
		pts := timing.SomePTS(timing.NewPTS(o.sampleOffset))
		duration := timing.SomeDuration(timing.NewDuration(samples))
		output.Own(&o.out, packet.NewChunk(o.sequence, pts, timing.SomeDTS(timing.NewDTS(o.sampleOffset)), duration, payload))
		err = output.Emit(ctx, &o.out)
		o.out.Drop()
		if err != nil {
			return err
		}
		o.sequence++
		o.sampleOffset += samples
	}
	return nil
}

func (*readerOperator) Flush(context.Context, flow.Emitter[packet.Chunk]) error { return nil }

type parserOperator struct {
	operatorBase
	out        flow.Item[packet.Packet]
	blockBytes int
}

func (o *parserOperator) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM parser received an unowned chunk")
	}
	if o.blockBytes == 0 || input.Value().Bytes().Len()%o.blockBytes != 0 {
		return ErrPartialSample
	}
	expectedDuration := timing.SomeDuration(timing.NewDuration(int64(input.Value().Bytes().Len() / o.blockBytes)))
	transferErr := flow.Transfer(input, &o.out, output, func(chunk packet.Chunk) (packet.Packet, error) {
		sequence, pts, dts, duration, sideData := chunk.Sequence(), chunk.PTS(), chunk.DTS(), chunk.Duration(), chunk.SideData()
		if duration.Valid() {
			if duration.Value() != expectedDuration.Value() {
				return packet.Packet{}, ErrDurationMismatch
			}
		} else {
			duration = expectedDuration
		}
		if !dts.Valid() {
			if value, ok := pts.Get(); ok {
				dts = timing.SomeDTS(timing.NewDTS(value.Int64()))
			}
		}
		return packet.NewPacket(sequence, pts, dts, duration, chunk.Detach()).WithSideData(sideData), nil
	})
	defer o.out.Drop()
	if transferErr != nil {
		return transferErr
	}
	return output.Emit(ctx, &o.out)
}

func (*parserOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

type writerOperator struct {
	operatorBase
	out flow.Item[access.Write]
}

func (o *writerOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[access.Write]) error {
	defer input.Drop()
	if !input.Value().Valid() {
		return errors.New("raw PCM writer received an invalid packet payload")
	}
	transferErr := flow.Transfer(input, &o.out, output, func(value packet.Packet) (access.Write, error) {
		return access.Append(value.Detach())
	})
	defer o.out.Drop()
	if transferErr != nil {
		return transferErr
	}
	return output.Emit(ctx, &o.out)
}

func (*writerOperator) Flush(context.Context, flow.Emitter[access.Write]) error { return nil }
