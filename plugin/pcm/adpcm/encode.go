package adpcm

import (
	"context"
	"encoding/binary"
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

// code writes one block from interleaved samples, chosen once at Open so a
// block never dispatches on the layout again.
type code func(block []byte, samples []int16) error

type encoderOperator struct {
	operatorBase
	out        flow.Item[packet.Packet]
	code       code
	parameters param.Parameters
	channels   int
	// pending holds the samples of a block that is not full yet. A coded block
	// is a fixed number of samples and a frame is whatever arrived, so the two
	// line up only by accident.
	pending  []int16
	held     int
	sequence uint64
	pts      timing.OptionalPTS
	block    []byte
}

func newEncoderOperator(plan componentPlan, buffers *buffer.Allocator) *encoderOperator {
	result := &encoderOperator{
		operatorBase: operatorBase{shape: plan.shape.Clone(), buffers: buffers},
		parameters:   plan.parameters,
		channels:     plan.channels,
		pending:      make([]int16, int(plan.parameters.SamplesPerBlock)*plan.channels),
		block:        make([]byte, plan.parameters.BlockAlign),
	}
	if plan.variant == Microsoft {
		result.code = func(block []byte, samples []int16) error {
			return msadpcm.EncodeBlock(block, samples, result.parameters, result.channels)
		}
	} else {
		state := &imaadpcm.EncodeState{}
		result.code = func(block []byte, samples []int16) error {
			return imaadpcm.EncodeBlock(block, samples, result.parameters, result.channels, state)
		}
	}
	return result
}

func (o *encoderOperator) Process(ctx context.Context, input *flow.Item[audio.Frame[int16]], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("ADPCM encoder received an unowned frame")
	}
	frame := input.Value()
	if frame.PlaneCount() != o.channels {
		return errors.New("ADPCM frame plane count does not match its channel layout")
	}
	if o.held == 0 {
		if value, ok := frame.PTS().Get(); ok {
			o.pts = timing.SomePTS(value)
		}
	}
	planes := make([][]int16, o.channels)
	for channel := range planes {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return err
		}
		planes[channel] = values.AppendTo(nil)
	}
	perBlock := int(o.parameters.SamplesPerBlock)
	for position := range frame.Samples() {
		for channel, plane := range planes {
			o.pending[o.held*o.channels+channel] = plane[position]
		}
		o.held++
		if o.held == perBlock {
			if err := o.emit(ctx, output, perBlock); err != nil {
				return err
			}
		}
	}
	return nil
}

// Flush states what the end of the input decided: the block still being filled
// is padded rather than dropped or shortened, because a coded block holds a
// fixed number of samples.
func (o *encoderOperator) Flush(ctx context.Context, output flow.Emitter[packet.Packet]) error {
	if o.held == 0 {
		return nil
	}
	held := o.held
	for index := o.held * o.channels; index < len(o.pending); index++ {
		o.pending[index] = 0
	}
	return o.emit(ctx, output, held)
}

func (o *encoderOperator) emit(ctx context.Context, output flow.Emitter[packet.Packet], samples int) error {
	if err := o.code(o.block, o.pending); err != nil {
		return err
	}
	payload, err := o.buffers.FromBytes(o.block, 1)
	if err != nil {
		return err
	}
	value := packet.NewPacket(o.sequence, o.pts, dtsFromPTS(o.pts),
		timing.SomeDuration(timing.NewDuration(int64(samples))), payload)
	output.Own(&o.out, value)
	defer o.out.Drop()
	if err := output.Emit(ctx, &o.out); err != nil {
		return err
	}
	o.sequence++
	o.held = 0
	if current, ok := o.pts.Get(); ok {
		o.pts = timing.SomePTS(timing.NewPTS(current.Int64() + int64(samples)))
	}
	return nil
}

func dtsFromPTS(pts timing.OptionalPTS) timing.OptionalDTS {
	value, ok := pts.Get()
	if !ok {
		return timing.UnknownDTS()
	}
	return timing.SomeDTS(timing.NewDTS(value.Int64()))
}

// marshalParameters writes the codec extension a container carries for this
// variant: how many samples a block holds, and for Microsoft the coefficient
// table its blocks index.
func marshalParameters(variant Variant, parameters param.Parameters) []byte {
	if variant == IMA {
		value := make([]byte, 2)
		binary.LittleEndian.PutUint16(value, parameters.SamplesPerBlock)
		return value
	}
	value := make([]byte, 4+len(parameters.Coefficients)*4)
	binary.LittleEndian.PutUint16(value[0:2], parameters.SamplesPerBlock)
	binary.LittleEndian.PutUint16(value[2:4], uint16(len(parameters.Coefficients)))
	for index, pair := range parameters.Coefficients {
		binary.LittleEndian.PutUint16(value[4+index*4:], uint16(pair.Coeff1))
		binary.LittleEndian.PutUint16(value[6+index*4:], uint16(pair.Coeff2))
	}
	return value
}
