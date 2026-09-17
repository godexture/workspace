package mp3

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/resource"
)

type packetSink struct {
	domain flow.Collector
	items  []*flow.Item[packet.Packet]
}

func (s *packetSink) Own(into *flow.Item[packet.Packet], value packet.Packet) {
	into.Bind(codec.Packets(), &s.domain)
	into.Set(value)
}

func (s *packetSink) Emit(_ context.Context, input *flow.Item[packet.Packet]) error {
	if !input.Valid() {
		return errors.New("packet sink received an unowned packet")
	}
	stored := new(flow.Item[packet.Packet])
	stored.Bind(codec.Packets(), &s.domain)
	if !stored.Move(input) {
		return errors.New("packet sink could not take packet ownership")
	}
	s.items = append(s.items, stored)
	return nil
}

func parserFor(t testing.TB, header [4]byte, freeBytes int) (*parser, *buffer.Allocator) {
	t.Helper()
	parsed, err := frame.Parse(header[:])
	if err != nil {
		t.Fatal(err)
	}
	maxFrame, ok := parsed.MaximumCompatibleFrameSize(freeBytes)
	if !ok {
		t.Fatal("header has no compatible frame bound")
	}
	value := framing{version: uint8(parsed.Version()), layer: uint8(parsed.Layer()), rate: parsed.SampleRateHz(), channels: 2, freeBytes: freeBytes, maxFrameBytes: maxFrame}
	allocator, err := buffer.NewAllocator(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	return newParser(parserPlan{shape: parserShape(), framing: value, hasFraming: true, signal: sample.Signal{Rate: parsed.SampleRateHz(), Layout: sample.Channels(2)}, maxFrame: maxFrame}, allocator), allocator
}

func parserChunk(t testing.TB, data []byte) *flow.Item[packet.Chunk] {
	t.Helper()
	allocator, err := buffer.NewAllocator(int64(len(data) + 1))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := allocator.FromBytes(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(packet.NewChunk(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload), format.Chunks(), &flow.Collector{})
	return &item
}

func closePackets(s *packetSink) {
	for _, item := range s.items {
		item.Drop()
	}
	s.items = nil
}

func TestParserFragmentsAndTimesFrames(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	data := mp3Audio(header, 3)
	parser, allocator := parserFor(t, header, 0)
	sink := &packetSink{}
	starts := []int{0, 1, 3, 22, 105, 522}
	ends := append(append([]int(nil), starts[1:]...), len(data))
	for index, start := range starts {
		if err := parser.Process(t.Context(), parserChunk(t, data[start:ends[index]]), sink); err != nil {
			t.Fatalf("Process(%d) = %v", index, err)
		}
	}
	if err := parser.Flush(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) != 3 {
		t.Fatalf("packets = %d, want 3", len(sink.items))
	}
	for index, item := range sink.items {
		value := item.Value()
		if value.Sequence() != uint64(index) || !value.PTS().Valid() || value.PTS().Value() != timing.PTS(index*1152) || value.DTS().Value() != timing.DTS(index*1152) || value.Duration().Value() != timing.Duration(1152) || !value.SideData().Empty() {
			t.Fatalf("packet %d = seq %d pts %v dts %v duration %v side %v", index, value.Sequence(), value.PTS(), value.DTS(), value.Duration(), value.SideData())
		}
	}
	if allocator.Used() == 0 {
		t.Fatal("parser output allocator did not retain packets")
	}
	closePackets(sink)
	if allocator.Used() != 0 {
		t.Fatalf("parser output allocator retained %d bytes", allocator.Used())
	}
}

func TestParserResyncMarksResetAndRejectsStreamChanges(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	data := append(mp3Audio(header, 2), []byte{0x01, 0x02, 0x03}...)
	data = append(data, mp3Audio(header, 2)...)
	parser, _ := parserFor(t, header, 0)
	sink := &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, data), sink); err != nil {
		t.Fatal(err)
	}
	if err := parser.Flush(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) != 3 {
		t.Fatalf("resync packets = %d, side data = %#v", len(sink.items), sink.items)
	}
	if _, ok := side.First(sink.items[0].Value().SideData(), resetKey); ok {
		t.Fatal("first packet unexpectedly carried a reset marker")
	}
	if reset, ok := side.First(sink.items[1].Value().SideData(), resetKey); !ok || !reset {
		t.Fatalf("recovered packet reset marker = %v, %v", reset, ok)
	}
	if _, ok := side.First(sink.items[2].Value().SideData(), resetKey); ok {
		t.Fatal("reset marker leaked past the first recovered packet")
	}
	closePackets(sink)

	changed := mp3Header(9, 1, false, frame.Stereo)
	parser, _ = parserFor(t, header, 0)
	sink = &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, mp3Audio(header, 3)), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) == 0 {
		t.Fatal("compatible prefix did not emit a packet")
	}
	if err := parser.Process(t.Context(), parserChunk(t, mp3Audio(changed, 2)), sink); !errors.Is(err, ErrMalformed) {
		t.Fatalf("changed stream error = %v", err)
	}
	closePackets(sink)
}

func TestParserRejectsIncompatibleLargeHeaderAtKnownBoundary(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	large := [4]byte{0xff, 0xf5, 0xe8, 0x00}
	if _, err := frame.Parse(large[:]); err != nil {
		t.Fatal(err)
	}
	parser, _ := parserFor(t, header, 0)
	sink := &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, mp3Audio(header, 3)), sink); err != nil {
		t.Fatal(err)
	}
	if err := parser.Flush(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) != 3 {
		t.Fatal("compatible prefix did not establish the stream framing")
	}
	if err := parser.Process(t.Context(), parserChunk(t, mp3Frame(large, 2881, 0)), sink); !errors.Is(err, ErrMalformed) {
		t.Fatalf("large incompatible header error = %v", err)
	}
	closePackets(sink)
}

func TestParserFreeFormatUsesInspectedHint(t *testing.T) {
	header := mp3Header(0, 0, false, frame.Stereo)
	data := append(mp3Frame(header, 300, 0), mp3Frame(header, 300, 1)...)
	data = append(data, mp3Frame(header, 300, 2)...)
	parser, allocator := parserFor(t, header, 300)
	sink := &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, data), sink); err != nil {
		t.Fatal(err)
	}
	if err := parser.Flush(t.Context(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.items) != 3 {
		t.Fatalf("free packets = %d", len(sink.items))
	}
	closePackets(sink)
	if allocator.Used() != 0 {
		t.Fatalf("free parser output allocator retained %d bytes", allocator.Used())
	}
}

func parserInput(t testing.TB, properties property.Set) stream.Descriptor {
	t.Helper()
	descriptor, err := stream.NewDescriptor("chunks", format.Chunks().Descriptor(), timing.MustBase(1, 44100), properties)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.WithMetadata(metadata.Absent())
}

func compileParser(t testing.TB, properties property.Set) (plugin.Compilation, error) {
	t.Helper()
	component := parserComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	return plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("chunks", parserInput(t, properties))))
}

func taggedSignal(t testing.TB) property.Set {
	t.Helper()
	signal := sample.Signal{Rate: 44100, Layout: sample.Channels(2)}
	properties, err := signal.Properties()
	if err != nil {
		t.Fatal(err)
	}
	properties, err = codec.WithTag(properties, CodecTag())
	if err != nil {
		t.Fatal(err)
	}
	return properties
}

func TestParserCompileWithoutPrivateFramingUsesTableBound(t *testing.T) {
	properties, err := stream.WithDuration(taggedSignal(t), timing.NewDuration(10*1152))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileParser(t, properties)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Resources().Memory != resource.Bytes(frame.MaximumTableFrameSize()) {
		t.Fatalf("parser memory = %d, want %d", compiled.Resources().Memory, frame.MaximumTableFrameSize())
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok {
		t.Fatal("parser compilation has no stream outputs")
	}
	output, ok := outputs.One("packets")
	if !ok {
		t.Fatal("parser compilation has no packet output")
	}
	if _, ok := framingOf(output.Properties()); ok {
		t.Fatal("private MP3 framing property leaked to parser output")
	}
	if tag, ok := codec.TagOf(output.Properties()); !ok || tag != CodecTag() {
		t.Fatalf("parser output codec tag = %q/%v", tag, ok)
	}
	if _, ok := stream.DurationOf(output.Properties()); ok {
		t.Fatal("parser preserved a duration that recovery can shorten")
	}
	effects := compiled.Effects()
	if len(effects) != 1 || effects[0].Kind != plugin.StructuralEffect || effects[0].Loss != plugin.UnknownLoss {
		t.Fatalf("parser effects = %#v", effects)
	}
}

func TestParserCompileRequiresExactCodecTag(t *testing.T) {
	properties := taggedSignal(t)
	properties = properties.Delete(codec.Tag().ID())
	if _, err := compileParser(t, properties); err == nil {
		t.Fatal("parser compiled without a codec tag")
	}
	wrong, err := codec.WithTag(taggedSignal(t), format.NewTag("other", "audio"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileParser(t, wrong); err == nil {
		t.Fatal("parser compiled with a wrong codec tag")
	}
}

func TestParserRejectsUnhintedFreeFormatAndPartialFinalFrame(t *testing.T) {
	header := mp3Header(0, 0, false, frame.Stereo)
	parsed, err := frame.Parse(header[:])
	if err != nil {
		t.Fatal(err)
	}
	parser, _ := parserFor(t, mp3Header(9, 0, false, frame.Stereo), 0)
	parser.framing = framing{version: uint8(parsed.Version()), layer: uint8(parsed.Layer()), rate: parsed.SampleRateHz(), channels: 2, maxFrameBytes: frame.MaximumTableFrameSize()}
	parser.maxFrame = frame.MaximumTableFrameSize()
	sink := &packetSink{}
	free := append(mp3Frame(header, 300, 0), mp3Frame(header, 300, 1)...)
	free = append(free, mp3Frame(header, 300, 2)...)
	if err := parser.Process(t.Context(), parserChunk(t, free), sink); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unhinted free-format error = %v", err)
	}
	closePackets(sink)

	parser, _ = parserFor(t, mp3Header(9, 0, false, frame.Stereo), 0)
	sink = &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, mp3Frame(mp3Header(9, 0, false, frame.Stereo), 100, 0)), sink); err != nil {
		t.Fatal(err)
	}
	if err := parser.Flush(t.Context(), sink); !errors.Is(err, ErrPartialFrame) {
		t.Fatalf("partial final frame error = %v", err)
	}
	closePackets(sink)
}

func TestParserEmitsCompleteFrameBeforePartialFinalFrame(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	parsed, err := frame.Parse(header[:])
	if err != nil {
		t.Fatal(err)
	}
	frameBytes, err := parsed.FrameSize(0)
	if err != nil {
		t.Fatal(err)
	}
	data := append(mp3Frame(header, frameBytes, 0), mp3Frame(header, 100, 1)...)
	parser, _ := parserFor(t, header, 0)
	sink := &packetSink{}
	if err := parser.Process(t.Context(), parserChunk(t, data), sink); err != nil {
		t.Fatal(err)
	}
	if err := parser.Flush(t.Context(), sink); !errors.Is(err, ErrPartialFrame) {
		t.Fatalf("partial final frame error = %v", err)
	}
	if len(sink.items) != 1 {
		t.Fatalf("packets before partial tail = %d, want 1", len(sink.items))
	}
	closePackets(sink)
}

func TestParserCancellationStopsRuntimeScan(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	parser, _ := parserFor(t, header, 0)
	sink := &packetSink{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := parser.Process(ctx, parserChunk(t, mp3Audio(header, 2)), sink); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled parser error = %v", err)
	}
	closePackets(sink)
}
