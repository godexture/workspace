package linear

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

var (
	ErrPartialSample = errors.New("linear PCM payload ends inside a sample frame")
	ErrPlaneCount    = errors.New("linear PCM frame plane count does not match its channel layout")
)

type operatorBase struct {
	shape   flow.Shape
	buffers *buffer.Allocator
}

func (o operatorBase) Ports() flow.Shape { return o.shape.Clone() }
func (operatorBase) Close() error        { return nil }

func openOperation(plan componentPlan, buffers *buffer.Allocator) (flow.Operator, error) {
	if buffers == nil && operationResources(plan.operation, plan.config).Memory != 0 {
		return nil, errors.New("linear PCM requires a payload buffer grant")
	}
	base := operatorBase{shape: plan.shape.Clone(), buffers: buffers}
	switch plan.operation {
	case readerOperation:
		return &readerOperator{operatorBase: base, configuration: plan.config}, nil
	case parserOperation:
		return &parserOperator{operatorBase: base, configuration: plan.config}, nil
	case decoderOperation:
		return &decoderOperator{operatorBase: base, configuration: plan.config}, nil
	case encoderOperation:
		return &encoderOperator{operatorBase: base, configuration: plan.config}, nil
	case writerOperation:
		return &writerOperator{operatorBase: base}, nil
	default:
		return nil, errors.New("unknown linear PCM operation")
	}
}

type readerOperator struct {
	operatorBase
	out           flow.Item[packet.Chunk]
	configuration configuration
	sequence      uint64
	sampleOffset  int64
}

func (o *readerOperator) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[packet.Chunk]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("raw PCM reader received unowned bytes")
	}
	data := input.Value().Bytes()
	frameBytes := o.configuration.Layout.Channels() * 2
	if frameBytes == 0 || data.Len()%frameBytes != 0 {
		return ErrPartialSample
	}
	chunkBytes := o.configuration.ChunkSamples * frameBytes
	for offset := 0; offset < data.Len(); offset += chunkBytes {
		end := offset + chunkBytes
		if end > data.Len() {
			end = data.Len()
		}
		payload, err := input.Value().Range(offset, end-offset)
		if err != nil {
			return err
		}
		samples := int64((end - offset) / frameBytes)
		output.Own(&o.out, packet.NewChunk(o.sequence, timing.SomePTS(timing.NewPTS(o.sampleOffset)), payload))
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
	out           flow.Item[packet.Packet]
	configuration configuration
}

func (o *parserOperator) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM parser received an unowned chunk")
	}
	frameBytes := o.configuration.Layout.Channels() * 2
	if frameBytes == 0 || input.Value().Bytes().Len()%frameBytes != 0 {
		return ErrPartialSample
	}
	transferErr := flow.Transfer(input, &o.out, output, func(chunk packet.Chunk) (packet.Packet, error) {
		duration := timing.SomeDuration(timing.NewDuration(int64(chunk.Bytes().Len() / frameBytes)))
		sequence, pts, sideData := chunk.Sequence(), chunk.PTS(), chunk.SideData()
		return packet.NewPacket(sequence, pts, timing.UnknownDTS(), duration, chunk.Detach()).WithSideData(sideData), nil
	})
	defer o.out.Drop()
	if transferErr != nil {
		return transferErr
	}
	return output.Emit(ctx, &o.out)
}

func (*parserOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

type decoderOperator struct {
	operatorBase
	out           flow.Item[audio.Frame[int16]]
	configuration configuration
	scratch       [pcmBlockBytes]byte
}

func (o *decoderOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[audio.Frame[int16]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM decoder received an unowned packet")
	}
	value := input.Value()
	channels := o.configuration.Layout.Channels()
	frameBytes := channels * 2
	if channels == 0 || value.Bytes().Len()%frameBytes != 0 {
		return ErrPartialSample
	}
	samples := value.Bytes().Len() / frameBytes
	planes := make([]buffer.PlaneSpec, channels)
	for index := range planes {
		planes[index].Size = samples * 2
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: planes})
	if err != nil {
		return err
	}
	defer lease.Discard()
	shift := uint(16 - o.configuration.ValidBits)
	encoded := value.Bytes()
	err = lease.Fill(func(storage buffer.Mutable) error {
		var destinations [2][]byte
		for channel := 0; channel < channels; channel++ {
			plane, err := storage.Plane(channel)
			if err != nil {
				return err
			}
			destinations[channel] = plane
		}
		return decodePCM(encoded, o.scratch[:], destinations, channels, samples, shift, o.configuration.Endian == sample.LittleEndian)
	})
	if err != nil {
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
	frame = frame.WithSideData(value.SideData())
	output.Own(&o.out, frame)
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

func (*decoderOperator) Flush(context.Context, flow.Emitter[audio.Frame[int16]]) error { return nil }

type encoderOperator struct {
	operatorBase
	out           flow.Item[packet.Packet]
	configuration configuration
	sequence      uint64
	scratch       [pcmBlockBytes / 2]int16
}

func (o *encoderOperator) Process(ctx context.Context, input *flow.Item[audio.Frame[int16]], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM encoder received an unowned frame")
	}
	frame := input.Value()
	channels := o.configuration.Layout.Channels()
	if len(frame.Planes().Layout().Planes) != channels {
		return ErrPlaneCount
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: frame.Samples() * channels * 2}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	shift := uint(16 - o.configuration.ValidBits)
	err = lease.Fill(func(storage buffer.Mutable) error {
		return encodePCM(frame, o.scratch[:], storage.Bytes(), channels, shift, o.configuration.Endian == sample.LittleEndian)
	})
	if err != nil {
		return err
	}
	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	packetValue := packet.NewPacket(
		o.sequence,
		frame.PTS(),
		timing.UnknownDTS(),
		timing.SomeDuration(timing.NewDuration(int64(frame.Samples()))),
		payload,
	).WithSideData(frame.SideData())
	output.Own(&o.out, packetValue)
	defer o.out.Drop()
	if err := output.Emit(ctx, &o.out); err != nil {
		return err
	}
	o.sequence++
	return nil
}

func (*encoderOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

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
