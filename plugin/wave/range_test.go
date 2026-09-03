package wave

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type rangeSourceSession struct {
	data    []byte
	mu      sync.Mutex
	reads   []int
	sizeErr error
}

func (s *rangeSourceSession) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.RandomRead, access.StableSize)
	return value
}

func (s *rangeSourceSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(destination, s.data[offset:])
	s.mu.Lock()
	s.reads = append(s.reads, len(destination))
	s.mu.Unlock()
	if n != len(destination) {
		return n, io.EOF
	}
	return n, nil
}

func (s *rangeSourceSession) Size(context.Context) (int64, error) {
	if s.sizeErr != nil {
		return 0, s.sizeErr
	}
	return int64(len(s.data)), nil
}

func (*rangeSourceSession) Close() error { return nil }

func rangeSourceOpening(t testing.TB, session *rangeSourceSession) access.Opening {
	t.Helper()
	selection, ok := access.Select(session.Capabilities(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)))
	if !ok {
		t.Fatal("source capabilities did not satisfy range requirements")
	}
	opening, err := access.NewOpening(access.SourceDirection, session, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	return opening
}

func TestRangeMuxReadsOpaqueAnchorsInFixedPages(t *testing.T) {
	unknown := bytes.Repeat([]byte{0xa5}, wavePageSize*2+17)
	data := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	source := testWAVE(data, 1, 48_000, testChunk{id: "BULK", payload: unknown})
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	header, err := newLinearRangeMuxHeader(inspected.description, inspected)
	if err != nil {
		t.Fatal(err)
	}
	session := &rangeSourceSession{data: source}
	operator := newMuxer(muxPlan{shape: muxerShape(), header: header}, mustBufferAllocator(t, wavePageSize*4))
	if err := operator.setSource(t.Context(), rangeSourceOpening(t, session)); err != nil {
		t.Fatal(err)
	}
	packetBuffers := mustBufferAllocator(t, len(data))
	handle, err := packetBuffers.FromBytes(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewItem(packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), handle), codec.Packets(), &testDomain)
	collector := &writeCollector{failAt: -1}
	if err := operator.Process(t.Context(), &input, collector); err != nil {
		t.Fatal(err)
	}
	if err := operator.finalize(); err != nil {
		t.Fatal(err)
	}
	if err := operator.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	encoded := applyWrites(t, collector.items)
	roundTrip, err := inspectHeaderWithSize(t.Context(), memoryRandom(encoded), uint64(len(encoded)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourceRangeBytes(t, encoded, roundTrip.ranges.beforeFormat); !bytes.Equal(got, sourceRangeBytes(t, source, inspected.ranges.beforeFormat)) {
		t.Fatal("opaque before-format range changed")
	}
	for _, size := range session.reads {
		if size > wavePageSize {
			t.Fatalf("source ReadAt requested %d bytes, page is %d", size, wavePageSize)
		}
	}
}

func TestRewriteInfoSourceKeepsUnknownFieldsAndPadding(t *testing.T) {
	title := infoTestChunk(t, "INAM", []byte("Song\x00"), 0x7f)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	carrier := infoTestList(t, title, unknown)
	// The source range model deliberately carries no raw block. Build the
	// edited semantic document independently, as a mux descriptor would.
	edited, err := metadata.Add(metadata.NewBuilder(metadata.StreamScope), tag.Title(), "Edited", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rewriteInfoSource(carrier, edited)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("Edited")) || !bytes.Contains(encoded, unknown) {
		t.Fatalf("rewritten INFO lost semantic or unknown field: %x", encoded)
	}
	if encoded[len(encoded)-1] != 0xcc {
		t.Fatalf("rewritten INFO lost unknown padding: %x", encoded)
	}
}

func TestRangeMuxOpenRequiresTheInspectedSource(t *testing.T) {
	unknown := bytes.Repeat([]byte{0x5a}, wavePageSize+3)
	source := testWAVE([]byte{1, 0}, 1, 48_000, testChunk{id: "BULK", payload: unknown})
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	compiled, component := compileRangeMux(t, inspected)
	_, err = component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: mustBufferAllocator(t, wavePageSize*2)}), compiled)
	if err == nil {
		t.Fatal("range mux opened without SourceOpening")
	}
}

func TestRangeMuxOpenRejectsTruncatedAndCanceledSource(t *testing.T) {
	unknown := bytes.Repeat([]byte{0x5a}, wavePageSize+3)
	source := testWAVE([]byte{1, 0}, 1, 48_000, testChunk{id: "BULK", payload: unknown})
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	compiled, component := compileRangeMux(t, inspected)
	shortEnd := inspected.ranges.beforeFormat.offset + inspected.ranges.beforeFormat.length - 1
	short := &rangeSourceSession{data: source[:shortEnd]}
	_, err = component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{
		Buffers: mustBufferAllocator(t, wavePageSize*2),
		Source:  rangeSourceOpening(t, short),
	}), compiled)
	if err == nil {
		t.Fatalf("truncated range source error = %v", err)
	}
	canceled := &rangeSourceSession{data: source, sizeErr: context.Canceled}
	_, err = component.Open(plugin.NewOpenContext(context.Background(), plugin.OpenServices{
		Buffers: mustBufferAllocator(t, wavePageSize*2),
		Source:  rangeSourceOpening(t, canceled),
	}), compiled)
	if err == nil {
		t.Fatalf("canceled range source error = %v", err)
	}
}

func TestRangeMuxStopsBeforeReadingACanceledSource(t *testing.T) {
	source := testWAVE([]byte{1, 0}, 1, 48_000, testChunk{id: "BULK", payload: bytes.Repeat([]byte{0x5a}, wavePageSize+3)})
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	header, err := newLinearRangeMuxHeader(inspected.description, inspected)
	if err != nil {
		t.Fatal(err)
	}
	session := &rangeSourceSession{data: source}
	operator := newMuxer(muxPlan{shape: muxerShape(), header: header}, mustBufferAllocator(t, wavePageSize))
	if err := operator.setSource(t.Context(), rangeSourceOpening(t, session)); err != nil {
		t.Fatal(err)
	}
	packetBuffers := mustBufferAllocator(t, 2)
	handle, err := packetBuffers.FromBytes([]byte{1, 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	input := flow.NewItem(packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), handle), codec.Packets(), &testDomain)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = operator.Process(ctx, &input, &writeCollector{failAt: -1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled range mux error = %v", err)
	}
	if len(session.reads) != 0 {
		t.Fatalf("canceled range mux read source: %v", session.reads)
	}
}

func compileRangeMux(t testing.TB, inspected header) (plugin.Compilation, plugin.Component) {
	t.Helper()
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	properties, err := inspected.description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("wave", codec.Packets().Descriptor(), timing.MustBase(1, int64(inspected.description.Rate)), properties).WithMetadata(inspected.metadataAttachment())
	compileContext, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(WAVE(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	return compiled, component
}

func mustBufferAllocator(t testing.TB, grant int) *buffer.Allocator {
	t.Helper()
	value, err := buffer.NewAllocator(int64(grant))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
