package linear

import (
	"context"
	"errors"
	"unsafe"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

// blockWords sizes the scratch every codec operator drains its input through.
// It is a uint64 array so the block is aligned for every canonical scalar.
const blockWords = 512

type scratch [blockWords]uint64

func (s *scratch) bytes() []byte { return unsafeBytes(s[:]) }

// codecOperator holds what the decoder and encoder share: the wire geometry,
// the aligned scratch, and the per-channel plane windows. Both are allocated
// once at Open so an item never allocates bookkeeping.
type codecOperator[S audio.Sample] struct {
	operatorBase
	description sample.Description
	channels    int
	blockBytes  int
	sampleBytes int
	swap        int
	scratch     scratch
	planes      []buffer.PlaneSpec
	windows     [][]S
}

func newCodecOperator[S audio.Sample](base operatorBase, description sample.Description, chunkSamples int) codecOperator[S] {
	channels := description.Layout.Count()
	result := codecOperator[S]{
		operatorBase: base,
		description:  description,
		channels:     channels,
		blockBytes:   description.BlockBytes(),
		sampleBytes:  sampleSize[S](),
		planes:       make([]buffer.PlaneSpec, channels),
		windows:      make([][]S, channels),
	}
	for index := range result.planes {
		result.planes[index].Size = chunkSamples * result.sampleBytes
	}
	return result
}

// blockLimit trims the scratch to whole interleaved sample frames so a channel
// always starts at the same offset inside every block.
func (o *codecOperator[S]) blockLimit() int {
	block := o.scratch.bytes()
	return len(block) - len(block)%o.blockBytes
}

type decoderOperator[S audio.Sample] struct {
	codecOperator[S]
	unpack unpack[S]
	out    flow.Item[audio.Frame[S]]
}

func (o *decoderOperator[S]) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[audio.Frame[S]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM decoder received an unowned packet")
	}
	value := input.Value()
	encoded := value.Bytes()
	if o.blockBytes == 0 || encoded.Len()%o.blockBytes != 0 {
		return ErrPartialSample
	}
	samples := encoded.Len() / o.blockBytes
	for index := range o.planes {
		o.planes[index].Size = samples * o.sampleBytes
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
	frame, err := audio.NewFrame[S](value.PTS(), samples, storage)
	if err != nil {
		storage.Release()
		return err
	}
	output.Own(&o.out, frame.WithSideData(value.SideData()))
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

// fill deinterleaves the packet into freshly leased planes. The wire is drained
// through the operator's scratch, so a payload the allocator split across
// blocks is read without ever exposing its backing storage.
func (o *decoderOperator[S]) fill(storage buffer.Mutable, encoded buffer.Bytes, samples int) error {
	planes := o.windows
	for channel := range planes {
		plane, err := storage.Plane(channel)
		if err != nil {
			return err
		}
		if planes[channel], err = audio.Plane[S](plane, samples); err != nil {
			return err
		}
	}
	limit := o.blockLimit()
	if limit == 0 {
		return ErrPartialSample
	}
	return encoded.Blocks(o.scratch.bytes()[:limit], func(block []byte, offset int) error {
		count := len(block) / o.blockBytes
		block = block[:count*o.blockBytes]
		if o.swap != 0 {
			if err := reverseSamples(block, o.swap); err != nil {
				return err
			}
		}
		first := offset / o.blockBytes
		for channel, plane := range planes {
			o.unpack(plane[first:first+count], block, channel, o.channels)
		}
		return nil
	})
}

func (*decoderOperator[S]) Flush(context.Context, flow.Emitter[audio.Frame[S]]) error { return nil }

type encoderOperator[S audio.Sample] struct {
	codecOperator[S]
	pack     pack[S]
	sequence uint64
	out      flow.Item[packet.Packet]
}

func (o *encoderOperator[S]) Process(ctx context.Context, input *flow.Item[audio.Frame[S]], output flow.Emitter[packet.Packet]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("linear PCM encoder received an unowned frame")
	}
	frame := input.Value()
	if len(frame.Planes().Layout().Planes) != o.channels {
		return ErrPlaneCount
	}
	samples := frame.Samples()
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: samples * o.blockBytes}}})
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
	value := packet.NewPacket(
		o.sequence,
		frame.PTS(),
		dtsFromPTS(frame.PTS()),
		timing.SomeDuration(timing.NewDuration(int64(samples))),
		payload,
	).WithSideData(frame.SideData())
	output.Own(&o.out, value)
	defer o.out.Drop()
	if err := output.Emit(ctx, &o.out); err != nil {
		return err
	}
	o.sequence++
	return nil
}

// fill interleaves the frame into the leased wire payload. Each plane is
// drained through the operator's scratch because a shared frame exposes its
// samples as an immutable view, not as a slice the codec may retype in place.
func (o *encoderOperator[S]) fill(target []byte, frame audio.Frame[S], samples int) error {
	limit := len(o.scratch.bytes())
	limit -= limit % o.sampleBytes
	if limit == 0 || len(target) < samples*o.blockBytes {
		return ErrPartialSample
	}
	for channel := range o.channels {
		plane, err := frame.Plane(channel)
		if err != nil {
			return err
		}
		occupied, err := plane.Slice(0, samples*o.sampleBytes)
		if err != nil {
			return err
		}
		err = occupied.Blocks(o.scratch.bytes()[:limit], func(block []byte, offset int) error {
			window, err := audio.Plane[S](block, len(block)/o.sampleBytes)
			if err != nil {
				return err
			}
			o.pack(target[offset/o.sampleBytes*o.blockBytes:], window, channel, o.channels)
			return nil
		})
		if err != nil {
			return err
		}
	}
	if o.swap != 0 {
		return reverseSamples(target, o.swap)
	}
	return nil
}

func (*encoderOperator[S]) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }

func dtsFromPTS(pts timing.OptionalPTS) timing.OptionalDTS {
	value, ok := pts.Get()
	if !ok {
		return timing.UnknownDTS()
	}
	return timing.SomeDTS(timing.NewDTS(value.Int64()))
}

func sampleSize[S audio.Sample]() int { return int(unsafe.Sizeof(*new(S))) }

func unsafeBytes(words []uint64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
}

// openCodec builds the decoder or encoder operator for frames of S. The wire
// coding chooses one pack or unpack loop here, so an item never dispatches on
// it again.
func openCodec[S audio.Sample](plan componentPlan, buffers *buffer.Allocator) (flow.Operator, error) {
	base := operatorBase{shape: plan.shape.Clone(), buffers: buffers}
	shared := newCodecOperator[S](base, plan.wire, plan.config.ChunkSamples)
	switch plan.operation {
	case decoderOperation:
		move, swap, err := newUnpack[S](plan.wire)
		if err != nil {
			return nil, err
		}
		shared.swap = swap
		return &decoderOperator[S]{codecOperator: shared, unpack: move}, nil
	case encoderOperation:
		move, swap, err := newPack[S](plan.wire)
		if err != nil {
			return nil, err
		}
		shared.swap = swap
		return &encoderOperator[S]{codecOperator: shared, pack: move}, nil
	default:
		return nil, errors.New("unknown linear PCM codec operation")
	}
}
