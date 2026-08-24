package audio

import (
	"context"
	"errors"
	"unsafe"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
)

var ErrUnsupported = errors.New("unsupported audio sample conversion")

// blockWords sizes the scratch each plane is drained through. It is a uint64
// array so the block is aligned for every canonical scalar.
const blockWords = 512

type operator[From, To mediaaudio.Sample] struct {
	shape    flow.Shape
	buffers  *buffer.Allocator
	channels int
	planes   []buffer.PlaneSpec
	scratch  [blockWords]uint64
	out      flow.Item[mediaaudio.Frame[To]]
}

func newOperator[From, To mediaaudio.Sample](plan convertPlan, buffers *buffer.Allocator) *operator[From, To] {
	return &operator[From, To]{
		shape:    plan.shape.Clone(),
		buffers:  buffers,
		channels: plan.channels,
		planes:   make([]buffer.PlaneSpec, plan.channels),
	}
}

func (o *operator[From, To]) Ports() flow.Shape { return o.shape.Clone() }
func (o *operator[From, To]) Close() error      { return nil }

func (o *operator[From, To]) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[From]], output flow.Emitter[mediaaudio.Frame[To]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("sample conversion received an unowned frame")
	}
	source := input.Value()
	if source.PlaneCount() != o.channels {
		return errors.New("sample conversion frame plane count does not match its channel layout")
	}
	samples := source.Samples()
	for index := range o.planes {
		o.planes[index].Size = samples * sampleSize[To]()
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: o.planes})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error { return o.fill(storage, source, samples) }); err != nil {
		return err
	}
	storage, err := lease.Commit()
	if err != nil {
		return err
	}
	frame, err := mediaaudio.NewFrame[To](source.PTS(), samples, storage)
	if err != nil {
		storage.Release()
		return err
	}
	output.Own(&o.out, frame.WithSideData(source.SideData()))
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

// fill converts each plane through the operator scratch. A shared frame exposes
// its samples as an immutable view, so the source is drained rather than
// retyped in place.
func (o *operator[From, To]) fill(storage buffer.Mutable, source mediaaudio.Frame[From], samples int) error {
	sourceSize := sampleSize[From]()
	block := o.scratchBytes()
	limit := len(block) - len(block)%sourceSize
	if limit == 0 {
		return ErrUnsupported
	}
	for channel := range o.channels {
		plane, err := storage.Plane(channel)
		if err != nil {
			return err
		}
		destination, err := mediaaudio.Plane[To](plane, samples)
		if err != nil {
			return err
		}
		occupied, err := source.Plane(channel)
		if err != nil {
			return err
		}
		occupied, err = occupied.Slice(0, samples*sourceSize)
		if err != nil {
			return err
		}
		err = occupied.Blocks(block[:limit], func(part []byte, offset int) error {
			values, err := mediaaudio.Plane[From](part, len(part)/sourceSize)
			if err != nil {
				return err
			}
			convert(destination[offset/sourceSize:], values)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (*operator[From, To]) Flush(context.Context, flow.Emitter[mediaaudio.Frame[To]]) error {
	return nil
}

func (o *operator[From, To]) scratchBytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&o.scratch[0])), len(o.scratch)*8)
}

func sampleSize[S mediaaudio.Sample]() int { return int(unsafe.Sizeof(*new(S))) }
