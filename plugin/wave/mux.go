package wave

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
)

type muxer struct {
	out           flow.Item[access.Write]
	shape         flow.Shape
	buffers       *buffer.Allocator
	header        muxHeader
	dataSize      uint64
	headerEmitted bool
	finalized     bool
	flushed       bool
}

func newMuxer(plan muxPlan, buffers *buffer.Allocator) *muxer {
	return &muxer{shape: plan.shape.Clone(), buffers: buffers, header: plan.header}
}

func (m *muxer) Ports() flow.Shape { return m.shape.Clone() }
func (*muxer) Close() error        { return nil }

func (m *muxer) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[access.Write]) error {
	defer input.Drop()
	if m.finalized {
		return errors.New("WAVE muxer received a packet after finalization")
	}
	if !input.Valid() || !input.Value().Valid() {
		return errors.New("WAVE muxer received an invalid packet")
	}
	payload := input.Value().Payload()
	size := payload.Bytes().Len()
	if uint64(size)%m.header.blockAlign != 0 {
		return ErrPartialBlock
	}
	if uint64(size) > math.MaxUint64-m.dataSize {
		return fmt.Errorf("%w: WAVE data size overflows", ErrUnsupported)
	}
	nextSize := m.dataSize + uint64(size)
	if _, _, err := m.header.outputSize(nextSize); err != nil {
		return err
	}
	if err := m.emitHeader(ctx, output); err != nil {
		return err
	}
	if err := flow.Transfer(input, &m.out, output, func(value packet.Packet) (access.Write, error) {
		return access.Append(value.Detach())
	}); err != nil {
		return err
	}
	defer m.out.Drop()
	if err := output.Emit(ctx, &m.out); err != nil {
		return err
	}
	m.dataSize = nextSize
	return nil
}

func (m *muxer) Finalize(context.Context) error {
	if m.finalized {
		return errors.New("WAVE muxer was finalized more than once")
	}
	if m.dataSize%m.header.blockAlign != 0 {
		return ErrPartialBlock
	}
	m.finalized = true
	return nil
}

func (m *muxer) Flush(ctx context.Context, output flow.Emitter[access.Write]) error {
	if !m.finalized {
		return errors.New("WAVE muxer must be finalized before flush")
	}
	if m.flushed {
		return nil
	}
	if err := m.emitHeader(ctx, output); err != nil {
		return err
	}
	finalized, err := m.header.finalize(m.dataSize)
	if err != nil {
		return err
	}
	if finalized.padding != 0 {
		if err := m.emitAppend(ctx, []byte{0}, output); err != nil {
			return err
		}
	}
	if len(m.header.afterData) != 0 {
		if err := m.emitAppend(ctx, m.header.afterData, output); err != nil {
			return err
		}
	}
	if len(m.header.trailer) != 0 {
		if err := m.emitAppend(ctx, m.header.trailer, output); err != nil {
			return err
		}
	}
	for _, patch := range finalized.patches {
		if err := m.emitPatch(ctx, patch, output); err != nil {
			return err
		}
	}
	m.flushed = true
	return nil
}

func (m *muxer) emitHeader(ctx context.Context, output flow.Emitter[access.Write]) error {
	if m.headerEmitted {
		return nil
	}
	if err := m.emitAppend(ctx, m.header.initial, output); err != nil {
		return err
	}
	m.headerEmitted = true
	return nil
}

func (m *muxer) emitAppend(ctx context.Context, payload []byte, output flow.Emitter[access.Write]) error {
	return m.emit(ctx, payload, func(handle buffer.Handle) (access.Write, error) {
		return access.Append(handle)
	}, output)
}

func (m *muxer) emitPatch(ctx context.Context, patch headerPatch, output flow.Emitter[access.Write]) error {
	return m.emit(ctx, patch.payload, func(handle buffer.Handle) (access.Write, error) {
		return access.Patch(patch.offset, handle)
	}, output)
}

func (m *muxer) emit(ctx context.Context, payload []byte, build func(buffer.Handle) (access.Write, error), output flow.Emitter[access.Write]) error {
	lease, err := m.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: len(payload)}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		copy(storage.Bytes(), payload)
		return nil
	}); err != nil {
		return err
	}
	handle, err := lease.Commit()
	if err != nil {
		return err
	}
	write, err := build(handle)
	if err != nil {
		return err
	}
	output.Own(&m.out, write)
	defer m.out.Drop()
	return output.Emit(ctx, &m.out)
}
