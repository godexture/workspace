package wave

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

type writeCollector struct {
	items  []flow.Input[access.Write]
	failAt int
}

func (c *writeCollector) Emit(_ context.Context, input flow.Input[access.Write]) error {
	if c.failAt >= 0 && len(c.items) == c.failAt {
		return errors.New("injected write emission failure")
	}
	c.items = append(c.items, input)
	return nil
}

func TestMuxEmitsHeaderPayloadAndFinalPatchesWithOwnedStorage(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian}
	header, err := newMuxHeader(description)
	if err != nil {
		t.Fatal(err)
	}
	muxBuffers, err := buffer.NewAllocator(512)
	if err != nil {
		t.Fatal(err)
	}
	packetBuffers, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	handle, err := packetBuffers.FromBytes(payload, 1)
	if err != nil {
		t.Fatal(err)
	}
	value := packet.NewPacket(0, timing.SomePTS(timing.NewPTS(0)), timing.UnknownDTS(), timing.SomeDuration(timing.NewDuration(2)), handle)
	operator := newMuxer(muxPlan{shape: muxerShape(), header: header}, muxBuffers)
	collector := &writeCollector{failAt: -1}
	if err := operator.Process(context.Background(), flow.NewInput(value, codec.Packets()), collector); err != nil {
		t.Fatal(err)
	}
	if err := operator.Finalize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := operator.Flush(context.Background(), collector); err != nil {
		t.Fatal(err)
	}
	encoded := applyWrites(t, collector.items)
	inspected, err := inspectHeader(context.Background(), memoryRandom(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if inspected.description != description || !bytes.Equal(encoded[inspected.dataOffset:int64(inspected.dataOffset)+int64(inspected.dataSize)], payload) {
		t.Fatalf("muxed WAVE = header %#v, bytes %v", inspected, encoded)
	}
	if muxBuffers.Used() != 0 || packetBuffers.Used() != 0 {
		t.Fatalf("mux retained payload = header %d, packet %d", muxBuffers.Used(), packetBuffers.Used())
	}
}

func TestMuxEmissionFailureLeavesPacketOwnedByCaller(t *testing.T) {
	description := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Mono, Endian: sample.LittleEndian}
	header, err := newMuxHeader(description)
	if err != nil {
		t.Fatal(err)
	}
	muxBuffers, _ := buffer.NewAllocator(256)
	packetBuffers, _ := buffer.NewAllocator(2)
	handle, err := packetBuffers.FromBytes([]byte{1, 2}, 1)
	if err != nil {
		t.Fatal(err)
	}
	value := packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), handle)
	input := flow.NewInput(value, codec.Packets())
	collector := &writeCollector{failAt: 1}
	operator := newMuxer(muxPlan{shape: muxerShape(), header: header}, muxBuffers)
	if err := operator.Process(context.Background(), input, collector); err == nil {
		input.Drop()
		t.Fatal("packet emission unexpectedly succeeded")
	}
	if packetBuffers.Used() == 0 || len(collector.items) != 1 {
		input.Drop()
		t.Fatalf("failed emission ownership = packet %d, writes %d", packetBuffers.Used(), len(collector.items))
	}
	input.Drop()
	for _, item := range collector.items {
		item.Drop()
	}
	if muxBuffers.Used() != 0 || packetBuffers.Used() != 0 {
		t.Fatalf("failure retained payload = header %d, packet %d", muxBuffers.Used(), packetBuffers.Used())
	}
}

func applyWrites(t *testing.T, items []flow.Input[access.Write]) []byte {
	t.Helper()
	var result []byte
	for _, item := range items {
		write := item.Value()
		switch write.Operation() {
		case access.AppendOperation:
			result = append(result, write.Bytes()...)
		case access.PatchOperation:
			end := int(write.Offset()) + len(write.Bytes())
			if end > len(result) {
				item.Drop()
				t.Fatalf("patch [%d,%d) exceeds %d emitted bytes", write.Offset(), end, len(result))
			}
			copy(result[int(write.Offset()):end], write.Bytes())
		default:
			item.Drop()
			t.Fatalf("unknown write operation %v", write.Operation())
		}
		item.Drop()
	}
	return result
}
