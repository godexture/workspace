package wave

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/packet"
)

type chunkCollector struct{ items []packet.Chunk }

func (c *chunkCollector) Emit(_ context.Context, input *flow.Item[packet.Chunk]) error {
	value, ok := input.Detach()
	if !ok {
		return errors.New("collector received an unowned chunk")
	}
	c.items = append(c.items, value)
	return nil
}

func TestDemuxRangesAlignedPayloadAndCopiesOnlyBoundaryFrame(t *testing.T) {
	payload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	encoded := testWAVE(payload, 2, 48_000, testChunk{id: "JUNK", payload: []byte{0xff}})
	inspected, err := inspectHeader(context.Background(), memoryRandom(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if inspected.dataOffset != 54 {
		t.Fatalf("data offset = %d", inspected.dataOffset)
	}
	sourceBuffers, err := buffer.NewAllocator(int64(len(encoded) * 2))
	if err != nil {
		t.Fatal(err)
	}
	reframeBuffers, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	operator := newDemuxer(demuxPlan{shape: demuxerShape(), header: inspected}, reframeBuffers)
	collector := &chunkCollector{}
	for _, part := range [][]byte{encoded[:56], encoded[56:]} {
		handle, err := sourceBuffers.FromBytes(part, 1)
		if err != nil {
			t.Fatal(err)
		}
		item := flow.NewItem(handle, access.Bytes())
		if err := operator.Process(context.Background(), &item, collector); err != nil {
			t.Fatal(err)
		}
	}
	if err := operator.Flush(context.Background(), collector); err != nil {
		t.Fatal(err)
	}
	if len(collector.items) != 2 {
		t.Fatalf("chunks = %d", len(collector.items))
	}
	var decoded []byte
	for _, item := range collector.items {
		if !item.Valid() {
			t.Fatalf("invalid chunk item = %#v", item)
		}
		decoded = item.Bytes().AppendTo(decoded)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("demux payload = %v", decoded)
	}
	if collector.items[0].Payload().Layout().ReadOnly || !collector.items[1].Payload().Layout().ReadOnly {
		t.Fatalf("copy/range layouts = %#v / %#v", collector.items[0].Payload().Layout(), collector.items[1].Payload().Layout())
	}
	for _, item := range collector.items {
		item.Release()
	}
	if sourceBuffers.Used() != 0 || reframeBuffers.Used() != 0 {
		t.Fatalf("retained payload = source %d, reframe %d", sourceBuffers.Used(), reframeBuffers.Used())
	}
}

func TestDemuxFlushRejectsTruncatedData(t *testing.T) {
	encoded := testWAVE([]byte{1, 2, 3, 4}, 1, 48_000)
	inspected, err := inspectHeader(context.Background(), memoryRandom(encoded))
	if err != nil {
		t.Fatal(err)
	}
	allocator, _ := buffer.NewAllocator(int64(len(encoded)))
	handle, err := allocator.FromBytes(encoded[:len(encoded)-2], 1)
	if err != nil {
		t.Fatal(err)
	}
	operator := newDemuxer(demuxPlan{shape: demuxerShape(), header: inspected}, allocator)
	collector := &chunkCollector{}
	truncatedItem := flow.NewItem(handle, access.Bytes())
	if err := operator.Process(context.Background(), &truncatedItem, collector); err != nil {
		t.Fatal(err)
	}
	if err := operator.Flush(context.Background(), collector); !errors.Is(err, ErrTruncatedData) {
		t.Fatalf("Flush error = %v", err)
	}
	for _, item := range collector.items {
		item.Release()
	}
}
