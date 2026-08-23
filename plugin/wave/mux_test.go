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
	items  []*flow.Item[access.Write]
	failAt int
}

func (*writeCollector) Own(into *flow.Item[access.Write], value access.Write) {
	into.Bind(access.Writes(), &testDomain)
	into.Set(value)
}

func (c *writeCollector) Emit(_ context.Context, input *flow.Item[access.Write]) error {
	if c.failAt >= 0 && len(c.items) == c.failAt {
		return errors.New("injected write emission failure")
	}
	if !input.Valid() {
		return errors.New("collector received an unowned write")
	}
	stored := new(flow.Item[access.Write])
	stored.Bind(access.Writes(), &testDomain)
	stored.Move(input)
	c.items = append(c.items, stored)
	return nil
}

func TestMuxEmitsHeaderPayloadAndFinalPatchesWithOwnedStorage(t *testing.T) {
	description := sample.Description{Signal: sample.Signal{Rate: 48_000, Layout: sample.Stereo(), ValidBits: 16}, Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.LittleEndian}
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
	packetItem := flow.NewItem(value, codec.Packets(), &testDomain)
	if err := operator.Process(context.Background(), &packetItem, collector); err != nil {
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

func TestMuxEmissionFailureReleasesEveryPayloadItAccepted(t *testing.T) {
	description := sample.Description{Signal: sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 16}, Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.LittleEndian}
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
	input := flow.NewItem(value, codec.Packets(), &testDomain)
	collector := &writeCollector{failAt: 1}
	operator := newMuxer(muxPlan{shape: muxerShape(), header: header}, muxBuffers)
	if err := operator.Process(context.Background(), &input, collector); err == nil {
		input.Drop()
		t.Fatal("packet emission unexpectedly succeeded")
	}
	if input.Valid() {
		input.Drop()
		t.Fatal("failed Process left its input unconsumed")
	}
	if len(collector.items) != 1 {
		t.Fatalf("failed emission accepted %d writes, want the header only", len(collector.items))
	}
	for _, item := range collector.items {
		item.Drop()
	}
	if muxBuffers.Used() != 0 || packetBuffers.Used() != 0 {
		t.Fatalf("failure retained payload = header %d, packet %d", muxBuffers.Used(), packetBuffers.Used())
	}
}

func applyWrites(t *testing.T, items []*flow.Item[access.Write]) []byte {
	t.Helper()
	var result []byte
	for _, item := range items {
		write := item.Value()
		switch write.Operation() {
		case access.AppendOperation:
			result = write.Bytes().AppendTo(result)
		case access.PatchOperation:
			end := int(write.Offset()) + write.Bytes().Len()
			if end > len(result) {
				item.Drop()
				t.Fatalf("patch [%d,%d) exceeds %d emitted bytes", write.Offset(), end, len(result))
			}
			write.Bytes().CopyTo(result[int(write.Offset()):end])
		default:
			item.Drop()
			t.Fatalf("unknown write operation %v", write.Operation())
		}
		item.Drop()
	}
	return result
}
