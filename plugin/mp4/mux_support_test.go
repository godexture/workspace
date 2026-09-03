package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type muxWriteCollector struct{ items []*flow.Item[access.Write] }

func (*muxWriteCollector) Own(into *flow.Item[access.Write], value access.Write) {
	into.Bind(access.Writes(), &componentTestDomain)
	into.Set(value)
}

func (c *muxWriteCollector) Emit(_ context.Context, input *flow.Item[access.Write]) error {
	if !input.Valid() {
		return errors.New("collector received an unowned write")
	}
	stored := new(flow.Item[access.Write])
	stored.Bind(access.Writes(), &componentTestDomain)
	stored.Move(input)
	c.items = append(c.items, stored)
	return nil
}

func applyMuxWrites(t testing.TB, items []*flow.Item[access.Write]) []byte {
	t.Helper()
	var result []byte
	for _, item := range items {
		write := item.Value()
		switch write.Operation() {
		case access.AppendOperation:
			result = write.Bytes().AppendTo(result)
		case access.PatchOperation:
			end := int(write.Offset()) + write.Bytes().Len()
			if write.Offset() < 0 || end > len(result) {
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

func compileMP4Mux(t testing.TB, inspected movie) (plugin.Component, plugin.Compilation) {
	t.Helper()
	compileContext, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]flow.PortDescriptor[stream.Descriptor], 0, len(inspected.tracks))
	for _, track := range inspected.tracks {
		properties, err := codec.WithTag(property.New(), SampleEntryTag(string(track.codec[:])))
		if err != nil {
			t.Fatal(err)
		}
		input := stream.MustDescriptor(trackStreamID(track.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(track.timeScale)), properties)
		if inspected.metadata.Scope().Valid() {
			input = input.WithMetadata(inspected.metadata)
		}
		inputs = append(inputs, flow.Describe("packets", input))
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(inputs...))
	if err != nil {
		t.Fatal(err)
	}
	return component, compiled
}

func openMP4Mux(t testing.TB, component plugin.Component, compiled plugin.Compilation, source access.Opening, buffers *buffer.Allocator, journal plugin.Scratch) *muxer {
	t.Helper()
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: buffers, Source: source, Scratch: journal}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := operator.(*muxer)
	if !ok {
		t.Fatal("MP4 mux Open did not return its muxer")
	}
	return value
}

func muxSample(t testing.TB, source []byte, value sample, allocator *buffer.Allocator) flow.Item[packet.Packet] {
	t.Helper()
	end := value.offset + uint64(value.size)
	if end > uint64(len(source)) {
		t.Fatal("test sample lies outside source")
	}
	handle, err := allocator.FromBytes(source[value.offset:end], 1)
	if err != nil {
		t.Fatal(err)
	}
	packetValue := packet.NewPacket(
		value.sequence-1,
		timing.SomePTS(timing.NewPTS(value.pts)),
		timing.SomeDTS(timing.NewDTS(int64(value.dts))),
		timing.SomeDuration(timing.NewDuration(int64(value.duration))),
		handle,
	)
	return flow.NewItem(packetValue, codec.Packets(), &componentTestDomain)
}

func movieSamples(t testing.TB, source []byte, value movie, track int) []sample {
	t.Helper()
	cursor, err := newSampleCursor(t.Context(), memoryRandom(source), value.tracks[track])
	if err != nil {
		t.Fatal(err)
	}
	var result []sample
	for {
		item, more, err := cursor.next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return result
		}
		result = append(result, item)
	}
}

type muxMemoryScratch struct {
	data      []byte
	appendErr error
	readErr   error
}

func (s *muxMemoryScratch) Append(_ context.Context, value []byte) (int64, error) {
	if s.appendErr != nil {
		return 0, s.appendErr
	}
	offset := int64(len(s.data))
	s.data = append(s.data, value...)
	return offset, nil
}

func (s *muxMemoryScratch) ReadAt(_ context.Context, target []byte, offset int64) error {
	if s.readErr != nil {
		return s.readErr
	}
	if offset < 0 || int64(len(target)) > int64(len(s.data))-offset {
		return errors.New("scratch read outside test extent")
	}
	copy(target, s.data[offset:])
	return nil
}

func (s *muxMemoryScratch) WriteAt(_ context.Context, value []byte, offset int64) error {
	if offset < 0 || int64(len(value)) > int64(len(s.data))-offset {
		return errors.New("scratch write outside test extent")
	}
	copy(s.data[offset:], value)
	return nil
}

func emptyTrackMovie() []byte {
	stbl := fixtureContainer("stbl",
		fixtureSTSD("avc1"),
		fixtureSTTS(nil),
		fixtureSTSC([]fixtureChunk{}),
		fixtureSTSZCount(0, 0),
		fixtureSTCOValues(nil),
	)
	return append(append(fixtureFileType("isom", "iso2"), fixtureContainer("moov", fixtureMVHD(), fixtureTrackWithTable(1, 1_000, "vide", stbl))...), fixtureBox("mdat", nil)...)
}

func reorderedTwoTrackMovie(t testing.TB, original []byte) []byte {
	t.Helper()
	parsed := inspectMovie(t, original)
	if len(parsed.tracks) != 2 || parsed.tracks[0].sampleCount != 1 || parsed.tracks[1].sampleCount != 1 {
		t.Fatal("reorder fixture is not two one-sample tracks")
	}
	result := append([]byte(nil), original...)
	payload := int(parsed.media.payloadOffset)
	copy(result[payload:payload+5], append(append([]byte(nil), original[payload+2:payload+5]...), original[payload:payload+2]...))
	firstOffset := parsed.media.payloadOffset + 3
	secondOffset := parsed.media.payloadOffset
	firstTable := parsed.tracks[0].tables.offsets.payloadOffset + 8
	secondTable := parsed.tracks[1].tables.offsets.payloadOffset + 8
	binary.BigEndian.PutUint32(result[firstTable:firstTable+4], uint32(firstOffset))
	binary.BigEndian.PutUint64(result[secondTable:secondTable+8], secondOffset)
	return result
}

func mustMP4Allocator(t testing.TB, limit int) *buffer.Allocator {
	t.Helper()
	value, err := buffer.NewAllocator(int64(limit))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var _ plugin.Scratch = (*muxMemoryScratch)(nil)
