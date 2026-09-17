package mp3

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/plugin"
)

type chunkSink struct {
	domain flow.Collector
	items  []*flow.Item[packet.Chunk]
}

func (s *chunkSink) Own(into *flow.Item[packet.Chunk], value packet.Chunk) {
	into.Bind(format.Chunks(), &s.domain)
	into.Set(value)
}

func (s *chunkSink) Emit(_ context.Context, input *flow.Item[packet.Chunk]) error {
	if !input.Valid() {
		return errors.New("chunk sink received an unowned chunk")
	}
	stored := new(flow.Item[packet.Chunk])
	stored.Bind(format.Chunks(), &s.domain)
	if !stored.Move(input) {
		return errors.New("chunk sink could not take chunk ownership")
	}
	s.items = append(s.items, stored)
	return nil
}

func closeChunks(s *chunkSink) {
	for _, item := range s.items {
		item.Drop()
	}
	s.items = nil
}

func TestReaderSplitsTaggedRangesAndFlushesAtSourceEnd(t *testing.T) {
	header := mp3Header(9, 0, false, 0)
	audio := mp3Audio(header, 2)
	leading := id3v2(2, 0, []byte("ab"), false)
	data := append(append(append([]byte(nil), leading...), audio...), id3v1()...)
	inspected := inspectTest(t, data, id3Prepared(t), 1<<20, 1<<20)
	reader := newReader(readerPlan{shape: readerShape(), inspection: inspected})
	allocator, err := buffer.NewAllocator(int64(len(data) * 2))
	if err != nil {
		t.Fatal(err)
	}
	sink := &chunkSink{}
	parts := [][]byte{
		data[:5],
		data[5 : len(leading)+7],
		data[len(leading)+7 : len(leading)+len(audio)+4],
		data[len(leading)+len(audio)+4:],
	}
	for index, part := range parts {
		handle, err := allocator.FromBytes(part, 1)
		if err != nil {
			t.Fatal(err)
		}
		input := flow.NewItem(handle, access.Bytes(), &flow.Collector{})
		if err := reader.Process(t.Context(), &input, sink); err != nil {
			t.Fatalf("Process(%d) = %v", index, err)
		}
		if index == 0 {
			if err := reader.Flush(t.Context(), sink); !errors.Is(err, ErrTruncated) {
				t.Fatalf("early Flush() = %v, want ErrTruncated", err)
			}
		}
	}
	if err := reader.Flush(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) != 2 {
		t.Fatalf("chunks = %d, want 2 audio ranges", len(sink.items))
	}
	var decoded []byte
	for index, item := range sink.items {
		value := item.Value()
		decoded = value.Bytes().AppendTo(decoded)
		layout := value.Payload().Layout()
		if value.Sequence() != uint64(index) || value.PTS().Valid() || value.DTS().Valid() || value.Duration().Valid() || !layout.ReadOnly || !layout.Shared {
			t.Fatalf("chunk %d = %#v", index, value)
		}
	}
	if !bytes.Equal(decoded, audio) {
		t.Fatal("reader emitted bytes outside the audio range or changed audio")
	}
	retained := int64(len(parts[1]) + len(parts[2]))
	if allocator.Used() != retained {
		t.Fatalf("reader allocator used = %d, want %d while outputs retain ranges", allocator.Used(), retained)
	}
	closeChunks(sink)
	if allocator.Used() != 0 {
		t.Fatalf("reader allocator retained %d bytes", allocator.Used())
	}
}

func TestReaderFlushRejectsExtraSourceBytes(t *testing.T) {
	header := mp3Header(9, 0, false, 0)
	audio := mp3Audio(header, 1)
	inspected := inspectTest(t, audio, plugin.CompileContext{}, 1<<20, 1<<20)
	reader := newReader(readerPlan{shape: readerShape(), inspection: inspected})
	allocator, err := buffer.NewAllocator(int64(len(audio) + 1))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes(append(audio, 0), 1)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewItem(handle, access.Bytes(), &flow.Collector{})
	sink := &chunkSink{}
	if err := reader.Process(t.Context(), &input, sink); err != nil {
		t.Fatal(err)
	}
	if err := reader.Flush(t.Context(), sink); !errors.Is(err, ErrMalformed) {
		t.Fatalf("extra-byte Flush() = %v, want ErrMalformed", err)
	}
	closeChunks(sink)
}
