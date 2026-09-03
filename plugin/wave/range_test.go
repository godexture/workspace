package wave

import (
	"bytes"
	"context"
	"encoding/binary"
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
	"github.com/godexture/godec/resource"
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
	encoded, err := rewriteInfoSource(carrier, detachedInfoDocument(t, carrier), edited)
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

func TestRewriteInfoSourceFollowsTargetEntryOrder(t *testing.T) {
	source := infoTestList(t,
		infoTestChunk(t, "INAM", []byte("Song\x00"), 0),
		infoTestChunk(t, "IART", []byte("Artist\x00"), 0),
	)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Artist(), "Artist", metadata.Origin{})
	metadata.Add(builder, tag.Title(), "Song", metadata.Origin{})
	target, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rewriteInfoSource(source, detachedInfoDocument(t, source), target)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := infoPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload[4:8]); got != "IART" {
		t.Fatalf("reordered first INFO child = %q, want IART", got)
	}
	firstSize := int(binary.LittleEndian.Uint32(payload[8:12]))
	second := 8 + firstSize + firstSize&1
	if got := string(payload[second+4 : second+8]); got != "INAM" {
		t.Fatalf("reordered second INFO child = %q, want INAM", got)
	}
}

func TestRewriteInfoSourceSwapsDuplicateEntriesAndKeepsInvalidOpaqueChildren(t *testing.T) {
	first := infoTestChunk(t, "IART", []byte("First\x00"), 0x71)
	invalid := infoTestChunk(t, "ICRD", []byte("not-a-date\x00"), 0x62)
	second := infoTestChunk(t, "IART", []byte("Second\x00"), 0xa4)
	unknown := infoTestChunk(t, "XTRA", []byte{1, 2, 3}, 0xcc)
	source := infoTestList(t, first, invalid, second, unknown)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Artist(), "Second", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "First", metadata.Origin{})
	date, err := tag.ParseDate("2026-08-10")
	if err != nil {
		t.Fatal(err)
	}
	metadata.Add(builder, tag.Date(), date, metadata.Origin{})
	target, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := rewriteInfoSource(source, detachedInfoDocument(t, source), target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, invalid) || !bytes.Contains(encoded, unknown) {
		t.Fatalf("rewrite lost invalid or unknown INFO child: %x", encoded)
	}
	payload, err := infoPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 5)
	var rawChildren [][]byte
	for offset := 4; offset < len(payload); {
		if len(payload)-offset < 8 {
			t.Fatalf("rewritten INFO child header truncated at %d", offset)
		}
		size := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		end, ok := infoChunkEnd(uint64(offset), size, uint64(len(payload)))
		if !ok || end > uint64(len(payload)) {
			t.Fatalf("rewritten INFO child at %d has invalid size %d", offset, size)
		}
		ids = append(ids, string(payload[offset:offset+4]))
		rawChildren = append(rawChildren, payload[offset:int(end)])
		offset = int(end)
	}
	wantIDs := []string{"IART", "ICRD", "IART", "XTRA", "ICRD"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("rewritten INFO child identities = %v, want %v", ids, wantIDs)
	}
	for index := range wantIDs {
		if ids[index] != wantIDs[index] {
			t.Fatalf("rewritten INFO child %d = %q, want %q", index, ids[index], wantIDs[index])
		}
	}
	if !bytes.Equal(rawChildren[1], invalid) || !bytes.Equal(rawChildren[3], unknown) {
		t.Fatalf("opaque INFO children moved or changed: invalid=%x unknown=%x", rawChildren[1], rawChildren[3])
	}
	if !bytes.Contains(rawChildren[0], []byte("Second")) || !bytes.Contains(rawChildren[2], []byte("First")) {
		t.Fatalf("duplicate swap target order = %q/%q", rawChildren[0], rawChildren[2])
	}
	parsed, err := infoTestResolver(t).Parse(t.Context(), RIFFInfo(), "rewritten", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", encoded))
	if err != nil {
		t.Fatal(err)
	}
	artists := metadata.Values(parsed, tag.Artist())
	if len(artists) != 2 || artists[0] != "Second" || artists[1] != "First" {
		t.Fatalf("duplicate swap semantic order = %v", artists)
	}
}

func TestLargeInfoRewriteUsesInspectionWorkspaceGrant(t *testing.T) {
	largeTitle := append(bytes.Repeat([]byte{'x'}, 70<<10), 0)
	info := infoTestList(t, infoTestChunk(t, "INAM", largeTitle, 0))
	source := waveTestRIFF(t,
		waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0),
		info,
		waveTestChunk(t, tagDATA, []byte{1, 0, 2, 0}, 0),
	)
	resolver := infoTestResolver(t)
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, resolver, resource.Bytes(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	inspectedMetadata := mustWaveMetadata(t, inspected.metadata)
	edited := inspectedMetadata.Edit()
	metadata.Add(edited, tag.Title(), "Edited", metadata.Origin{})
	document, err := edited.Build()
	if err != nil {
		t.Fatal(err)
	}
	workspace, output, err := infoRewriteWorkspaceAgainst(inspected.ranges.info.length, inspectedMetadata, document)
	if err != nil {
		t.Fatal(err)
	}
	if workspace <= inspected.ranges.info.length || output <= inspected.ranges.info.length {
		t.Fatalf("workspace upper bound = %d, output %d, source %d", workspace, output, inspected.ranges.info.length)
	}
	if want := inspected.ranges.info.length + output; workspace != want {
		t.Fatalf("rewrite workspace = %d, want source-plus-output %d", workspace, want)
	}
	compiled, component, componentErr := compileWaveMuxMetadataStateWithComponent(t, inspected, metadata.MustAvailable(document))
	if componentErr != nil {
		t.Fatal(componentErr)
	}
	rangeHeader, err := newLinearRangeMuxHeader(inspected.description, inspected)
	if err != nil {
		t.Fatal(err)
	}
	wantMemory := uint64(max(rangeHeader.payloadBytes(), wavePageSize)) + workspace
	if uint64(compiled.Resources().Memory) != wantMemory {
		t.Fatalf("large rewrite grant = %d, want %d", compiled.Resources().Memory, wantMemory)
	}
	muxBuffers := mustBufferAllocator(t, int(compiled.Resources().Memory))
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{
		Buffers: muxBuffers, Source: rangeSourceOpening(t, &rangeSourceSession{data: source}),
	}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	packetBuffers := mustBufferAllocator(t, 4)
	handle, err := packetBuffers.FromBytes([]byte{1, 0, 2, 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(packet.NewPacket(0, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), handle), codec.Packets(), &testDomain)
	collector := &writeCollector{failAt: -1}
	mux := operator.(*muxer)
	if err := mux.Process(t.Context(), &item, collector); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	encoded := applyWrites(t, collector.items)
	if !bytes.Contains(encoded, []byte("Edited")) || !bytes.Contains(encoded, largeTitle) {
		t.Fatalf("large rewrite lost edited/retained INFO bytes")
	}
	if _, err := inspectHeaderWithSize(t.Context(), memoryRandom(encoded), uint64(len(encoded)), true, resolver, resource.Bytes(1<<20)); err != nil {
		t.Fatalf("rewritten large INFO did not re-inspect: %v", err)
	}
	if err := operator.Close(); err != nil {
		t.Fatalf("large rewrite Close = %v", err)
	}
	if muxBuffers.Used() != 0 {
		t.Fatalf("large rewrite retained allocator bytes after Close: %d", muxBuffers.Used())
	}

	earlyBuffers := mustBufferAllocator(t, int(compiled.Resources().Memory))
	early, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{
		Buffers: earlyBuffers, Source: rangeSourceOpening(t, &rangeSourceSession{data: source}),
	}), compiled)
	if err != nil {
		t.Fatalf("large rewrite early Open = %v", err)
	}
	if earlyBuffers.Used() == 0 {
		t.Fatal("large rewrite early Open retained no replacement workspace")
	}
	if err := early.Close(); err != nil {
		t.Fatalf("large rewrite early Close = %v", err)
	}
	if earlyBuffers.Used() != 0 {
		t.Fatalf("large rewrite early Close retained allocator bytes: %d", earlyBuffers.Used())
	}

	inspected.infoMemoryLimit = workspace - 1
	if _, err := compileWaveMuxMetadataState(t, inspected, metadata.MustAvailable(document)); err == nil {
		t.Fatalf("one-byte-short rewrite workspace = %v, want unsupported", err)
	}
}

func TestLargeInfoUnchangedRemuxKeepsPageGrant(t *testing.T) {
	unknown := infoTestChunk(t, "XTRA", bytes.Repeat([]byte{0xa5}, 70<<10), 0xcc)
	info := infoTestList(t, infoTestChunk(t, "INAM", []byte("Song\x00"), 0), unknown)
	source := waveTestRIFF(t,
		waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0),
		info,
		waveTestChunk(t, tagDATA, []byte{1, 0}, 0),
	)
	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), resource.Bytes(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	compiled, component := compileRangeMux(t, inspected)
	if compiled.Resources().Memory != resource.Bytes(wavePageSize) {
		t.Fatalf("unchanged large INFO grant = %d, want %d", compiled.Resources().Memory, wavePageSize)
	}
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{
		Buffers: mustBufferAllocator(t, wavePageSize), Source: rangeSourceOpening(t, &rangeSourceSession{data: source}),
	}), compiled)
	if err != nil {
		t.Fatalf("unchanged large INFO Open = %v", err)
	}
	if err := operator.Close(); err != nil {
		t.Fatalf("unchanged large INFO Close = %v", err)
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

func detachedInfoDocument(t testing.TB, value []byte) metadata.Document {
	t.Helper()
	document, err := infoTestResolver(t).Parse(t.Context(), RIFFInfo(), "rewrite", metadata.StreamScope, metadata.NewBlob("application/x-riff-info", value))
	if err != nil {
		t.Fatal(err)
	}
	detached, err := document.DetachSource()
	if err == nil {
		return detached
	}
	// The range rewrite tests intentionally retain opaque source children in
	// the wire value while supplying only the semantic source projection.
	builder := metadata.NewBuilder(metadata.StreamScope)
	copyInfoEntries(builder, document.Entries(), false)
	detached, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return detached
}
