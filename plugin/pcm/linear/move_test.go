package linear

import (
	"context"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/timing"
)

type packetSink struct{ items int }

func (*packetSink) Own(into *flow.Item[packet.Packet], value packet.Packet) {
	into.Bind(codec.Packets(), &testDomain)
	into.Set(value)
}

func (s *packetSink) Emit(_ context.Context, item *flow.Item[packet.Packet]) error {
	s.items++
	item.Drop()
	return nil
}

type writeSink struct{ items int }

func (*writeSink) Own(into *flow.Item[access.Write], value access.Write) {
	into.Bind(access.Writes(), &testDomain)
	into.Set(value)
}

func (s *writeSink) Emit(_ context.Context, item *flow.Item[access.Write]) error {
	s.items++
	item.Drop()
	return nil
}

// A stage that only rewraps a payload for the next item type must move it.
// Retaining instead would allocate one lease per item per hop, which the
// hot-path contract forbids for a linear ownership transfer.
func TestPayloadRewrappingHopsAllocateNothing(t *testing.T) {
	configuration := configuration{Layout: "stereo", ValidBits: 16, Rate: 48_000, Endian: "little", ChunkSamples: 4}

	t.Run("parser", func(t *testing.T) {
		allocator, err := buffer.NewAllocator(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		operator := &parserOperator{operatorBase: operatorBase{buffers: allocator}, configuration: configuration}
		sink := &packetSink{}
		var cell flow.Item[packet.Chunk]
		cell.Bind(format.Chunks(), &testDomain)
		fill := func() {
			payload, allocErr := allocator.FromBytes(make([]byte, 16), 1)
			if allocErr != nil {
				panic(allocErr)
			}
			cell.Set(packet.NewChunk(0, timing.SomePTS(timing.NewPTS(0)), timing.SomeDTS(timing.NewDTS(0)), timing.SomeDuration(timing.NewDuration(4)), payload))
		}
		assertHopIsFree(t, allocator, fill, func() { cell.Drop() }, func() error {
			return operator.Process(context.Background(), &cell, sink)
		})
	})

	t.Run("writer", func(t *testing.T) {
		allocator, err := buffer.NewAllocator(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		operator := &writerOperator{operatorBase: operatorBase{buffers: allocator}}
		sink := &writeSink{}
		var cell flow.Item[packet.Packet]
		cell.Bind(codec.Packets(), &testDomain)
		fill := func() {
			payload, allocErr := allocator.FromBytes(make([]byte, 16), 1)
			if allocErr != nil {
				panic(allocErr)
			}
			cell.Set(packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload))
		}
		assertHopIsFree(t, allocator, fill, func() { cell.Drop() }, func() error {
			return operator.Process(context.Background(), &cell, sink)
		})
	})
}

// assertHopIsFree compares the fixture cost alone against the fixture plus one
// Process, so only the hop's own allocations are measured.
func assertHopIsFree(t *testing.T, allocator *buffer.Allocator, fill, drop func(), process func() error) {
	t.Helper()
	baseline := testing.AllocsPerRun(500, func() {
		fill()
		drop()
	})
	total := testing.AllocsPerRun(500, func() {
		fill()
		if err := process(); err != nil {
			panic(err)
		}
	})
	if hop := total - baseline; hop != 0 {
		t.Errorf("hop allocations = %v (fixture %v, with Process %v), want 0", hop, baseline, total)
	}
	if allocator.Used() != 0 {
		t.Errorf("operator retained %d payload bytes", allocator.Used())
	}
}
