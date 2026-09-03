package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/scratch"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type muxSpyEncodingID struct{}
type muxSpyKeyID struct{}

var muxSpyKey = key.Define[muxSpyKeyID, string]()

func TestMP4IlstMuxRewritesConsensusDocument(t *testing.T) {
	unknown := ilstTestItem(ilstType{'-', '-', '-', '-'}, []byte{0xde, 0xad, 0xbe, 0xef})
	data := ilstMovieFixture("mdir", unknown, ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	edited := muxEditedTitle(t, inspected.metadata, "Edited")
	resolver := ilstTestResolver(t, IlstCarrier())
	prepared, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = mediaformat.WithInspection(prepared, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	inputs := muxMetadataInputs(t, inspected, edited)
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, prepared, resolved, flow.NewDescriptors(inputs...))
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.MetadataReports()) != 0 {
		t.Fatalf("edited representable metadata reports = %#v", compiled.MetadataReports())
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok || len(outputs.At("writes")) != 1 || !sameIlstMuxDocument(outputs.At("writes")[0].Metadata(), edited) {
		t.Fatalf("mux output metadata = %#v", outputs)
	}

	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
	packets := mustMP4Allocator(t, 8)
	collector := &muxWriteCollector{}
	for ordinal := range inspected.tracks {
		for _, sample := range movieSamples(t, data, inspected, ordinal) {
			item := muxSample(t, data, sample, packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &item), collector); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	encoded := applyMuxWrites(t, collector.items)
	if bytes.Equal(encoded, data) {
		t.Fatal("edited metadata remux remained byte exact")
	}
	result := inspectMovieWithIlst(t, encoded)
	if title, ok := metadata.First(result.metadata, tag.Title()); !ok || title != "Edited" {
		t.Fatalf("rewritten title = %q/%v", title, ok)
	}
	opaque, ok := result.metadata.Block(ilstItemBlockID(result.ilst.block, 0))
	if !ok || opaque.Source() || !opaque.Payload().Equal(metadata.NewBlob(ilstItemMediaType, unknown)) {
		t.Fatalf("rewritten opaque block = %#v/%v", opaque, ok)
	}
}

func TestMP4IlstMuxUsesConsensusAndMarshalsOnce(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	resolver, calls := muxSpyResolver(t)
	unchanged := muxMetadataInputs(t, inspected, inspected.metadata)
	if _, err := compileMuxWithResolver(t.Context(), resolver, descriptorsToInputs(unchanged), inspected); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("unchanged metadata marshal calls = %d, want 0", *calls)
	}

	edited := muxEditedTitle(t, inspected.metadata, "Edited")
	changed := muxMetadataInputs(t, inspected, edited)
	layout, err := compileMuxWithResolver(t.Context(), resolver, descriptorsToInputs(changed), inspected)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || !layout.rewrite.active || len(layout.reports) != 0 {
		t.Fatalf("changed metadata calls/layout = %d/%#v", *calls, layout)
	}
	for _, piece := range layout.pieces {
		if piece.kind == muxBlob && piece.blob.Len() != 0 {
			t.Fatal("spy metadata payload was not retained as a blob piece")
		}
	}
}

func TestMP4IlstMuxCombinesRewriteWithTrackSubset(t *testing.T) {
	for _, test := range []struct {
		name     string
		selected int
		offsets  boxType
	}{
		{name: "stco", selected: 0, offsets: typeSTCO},
		{name: "co64", selected: 1, offsets: typeCO64},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := ilstMovieFixtureAfterMdat("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
			inspected := inspectMovieWithIlst(t, data)
			selected := inspected.tracks[test.selected]
			encoded := runIlstRewriteSubsetMux(t, data, "Edited", test.selected)
			result := inspectMovieWithIlst(t, encoded)
			if len(result.tracks) != 1 || result.tracks[0].id != selected.id {
				t.Fatalf("subset metadata tracks = %#v, want track %d", result.tracks, selected.id)
			}
			if title, ok := metadata.First(result.metadata, tag.Title()); !ok || title != "Edited" {
				t.Fatalf("subset rewritten title = %q/%v", title, ok)
			}
			if result.media.payloadSize != selected.sampleBytes {
				t.Fatalf("subset metadata payload size = %d, want %d", result.media.payloadSize, selected.sampleBytes)
			}
			samples := movieSamples(t, data, inspected, test.selected)
			if len(samples) != 1 {
				t.Fatalf("subset source samples = %d, want one", len(samples))
			}
			sample := samples[0]
			if !bytes.Equal(encoded[result.media.payloadOffset:result.media.payloadOffset+uint64(sample.size)], data[sample.offset:sample.offset+uint64(sample.size)]) {
				t.Fatal("subset metadata rewrite changed the selected sample bytes")
			}
			if result.tracks[0].tables.offsets.typeID != test.offsets {
				t.Fatalf("subset offset table = %q, want %q", result.tracks[0].tables.offsets.typeID, test.offsets)
			}
			tableOffset := result.tracks[0].tables.offsets.payloadOffset + 8
			var got uint64
			if test.offsets == typeCO64 {
				got = binary.BigEndian.Uint64(encoded[tableOffset:])
			} else {
				got = uint64(binary.BigEndian.Uint32(encoded[tableOffset:]))
			}
			if got != result.media.payloadOffset {
				t.Fatalf("subset chunk offset = %d, want mdat payload %d", got, result.media.payloadOffset)
			}
		})
	}
}

func TestMP4MuxResizedBoxPreservesLargeHeader(t *testing.T) {
	large := box{headerSize: 16, size: 16 + 32, payloadSize: 32}
	if size, header, ok := resizedBox(large, 1); !ok || size != 17 || header != 16 {
		t.Fatalf("large header shrink = %d/%d/%v", size, header, ok)
	}
	normal := box{headerSize: 8, size: 8 + math.MaxUint32 - 8, payloadSize: math.MaxUint32 - 8}
	if size, header, ok := resizedBox(normal, math.MaxUint32-7); !ok || size != uint64(math.MaxUint32-7)+16 || header != 16 {
		t.Fatalf("normal header expansion = %d/%d/%v", size, header, ok)
	}
	open := box{headerSize: 8, size: 24, payloadSize: 16, openEnded: true}
	if size, header, ok := resizedBox(open, 8); !ok || size != 16 || header != 8 {
		t.Fatalf("open-ended box compaction = %d/%d/%v, want 16/8/true", size, header, ok)
	}
}

func TestMP4MuxEmitsMetadataBlobPagewise(t *testing.T) {
	payload := bytes.Repeat([]byte{0x7a}, muxPageBytes+17)
	mux := &muxer{buffers: mustMP4Allocator(t, 2*muxPageBytes)}
	collector := &muxWriteCollector{}
	if err := mux.emitBlob(t.Context(), metadata.NewBlob(ilstMediaType, payload), collector); err != nil {
		t.Fatal(err)
	}
	if len(collector.items) != 2 {
		t.Fatalf("metadata blob writes = %d, want two pages", len(collector.items))
	}
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, payload) {
		t.Fatal("pagewise metadata blob changed payload")
	}
}

func TestMP4IlstMuxRejectsConsensusMismatchBeforeMarshal(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	first := muxEditedTitle(t, inspected.metadata, "First")
	second := muxEditedTitle(t, inspected.metadata, "Second")
	inputs := muxMetadataInputs(t, inspected, first)
	inputs[1] = flow.Describe("packets", inputs[1].Descriptor().WithMetadata(second))
	resolver, calls := muxSpyResolver(t)
	if _, err := compileMuxWithResolver(t.Context(), resolver, descriptorsToInputs(inputs), inspected); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("metadata mismatch error = %v", err)
	}
	if *calls != 0 {
		t.Fatalf("metadata mismatch marshal calls = %d, want 0", *calls)
	}
}

func TestMP4IlstMuxRejectsDroppedInspectedOpaqueBlock(t *testing.T) {
	unknown := ilstTestItem(ilstType{'-', '-', '-', '-'}, []byte{1, 2, 3})
	data := ilstMovieFixture("mdir", unknown, ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	if len(inspected.metadata.Blocks()) != 2 {
		t.Fatalf("opaque fixture blocks = %#v", inspected.metadata.Blocks())
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	for _, block := range inspected.metadata.Blocks() {
		if block.Source() {
			builder.AddBlock(block)
		}
	}
	entry := inspected.metadata.Entries()[0]
	metadata.Add(builder, tag.Title(), "Edited", entry.Origin())
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	resolver, calls := muxSpyResolver(t)
	if _, err := compileMuxWithResolver(t.Context(), resolver, descriptorsToInputs(muxMetadataInputs(t, inspected, document)), inspected); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("dropped opaque error = %v", err)
	}
	if *calls != 0 {
		t.Fatalf("dropped opaque marshal calls = %d, want 0", *calls)
	}
}

func TestMP4IlstMuxMissingBindingUsesStructuredDiagnostic(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	edited := muxEditedTitle(t, inspected.metadata, "Edited")
	resolver := metadata.Resolver{}
	var err error
	_, err = compileMuxWithResolver(t.Context(), resolver, descriptorsToInputs(muxMetadataInputs(t, inspected, edited)), inspected)
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "metadata.binding-missing" && item.Detail["carrier"] == IlstCarrier().String() {
			return
		}
	}
	t.Fatalf("missing ilst mux binding diagnostic = %v", err)
}

func TestMP4IlstMuxResourceRetainsRewriteBlob(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	edited := muxEditedTitle(t, inspected.metadata, "Edited")
	payload := metadata.NewBlob(ilstMediaType, bytes.Repeat([]byte{0x6c}, muxPageBytes+1))
	resolver, _ := muxSpyResolverWithPayload(t, payload)
	prepared, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = mediaformat.WithInspection(prepared, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, prepared, resolved, flow.NewDescriptors(muxMetadataInputs(t, inspected, edited)...))
	if err != nil {
		t.Fatal(err)
	}
	want := resource.Bytes(payload.Len())
	if compiled.Resources().Memory != want {
		t.Fatalf("rewrite payload grant = %d, want %d", compiled.Resources().Memory, want)
	}
}

func TestMP4IlstRewritePreservesOpenEndedMdatHeader(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	initial := inspectMovieWithIlst(t, data)
	binary.BigEndian.PutUint32(data[initial.media.offset:], 0)
	encoded := runIlstRewriteMux(t, data, "Edited")
	result := inspectMovieWithIlst(t, encoded)
	if !result.media.openEnded || binary.BigEndian.Uint32(encoded[result.media.offset:]) != 0 {
		t.Fatalf("full metadata rewrite did not preserve zero-size mdat header: %#v", result.media)
	}
}

func TestMP4MuxSubsetCompactsOpenEndedTopLevelHeaders(t *testing.T) {
	for _, test := range []struct {
		name      string
		afterMdat bool
		zero      func(movie, []byte)
	}{{
		name: "terminal mdat",
		zero: func(value movie, data []byte) { binary.BigEndian.PutUint32(data[value.media.offset:], 0) },
	}, {
		name:      "terminal moov",
		afterMdat: true,
		zero:      func(value movie, data []byte) { binary.BigEndian.PutUint32(data[value.moov.offset:], 0) },
	}} {
		t.Run(test.name, func(t *testing.T) {
			data := twoTrackMovie(test.afterMdat, "isom", "iso2")
			initial := inspectMovie(t, data)
			test.zero(initial, data)
			encoded := runSubsetMux(t, data, 0)
			result := inspectMovie(t, encoded)
			if result.moov.openEnded || result.media.openEnded {
				t.Fatalf("subset retained zero-size top-level header: moov=%#v media=%#v", result.moov, result.media)
			}
			if binary.BigEndian.Uint32(encoded[result.moov.offset:]) != uint32(result.moov.size) || binary.BigEndian.Uint32(encoded[result.media.offset:]) != uint32(result.media.size) {
				t.Fatalf("subset top-level headers do not encode finite sizes: moov=%#v media=%#v", result.moov, result.media)
			}
		})
	}
}

func ilstMovieFixtureAfterMdat(handler string, items ...[]byte) []byte {
	tracks := []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, largeOffset: true, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}
	return fixtureMovie(true, "isom", []string{"iso2"}, tracks, nil, [][]byte{ilstMetaFixture(handler, fixtureBox("ilst", bytes.Join(items, nil)))})
}

func runIlstRewriteMux(t testing.TB, data []byte, title string) []byte {
	t.Helper()
	inspected := inspectMovieWithIlst(t, data)
	edited := muxEditedTitle(t, inspected.metadata, title)
	resolver := ilstTestResolver(t, IlstCarrier())
	prepared, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = mediaformat.WithInspection(prepared, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, prepared, resolved, flow.NewDescriptors(muxMetadataInputs(t, inspected, edited)...))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
	packets := mustMP4Allocator(t, 8)
	collector := &muxWriteCollector{}
	for ordinal := range inspected.tracks {
		for _, sample := range movieSamples(t, data, inspected, ordinal) {
			input := muxSample(t, data, sample, packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	return applyMuxWrites(t, collector.items)
}

func runIlstRewriteSubsetMux(t testing.TB, data []byte, title string, selectedIndex int) []byte {
	t.Helper()
	inspected := inspectMovieWithIlst(t, data)
	edited := muxEditedTitle(t, inspected.metadata, title)
	resolver := ilstTestResolver(t, IlstCarrier())
	prepared, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err = mediaformat.WithInspection(prepared, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	inputs := muxMetadataInputs(t, inspected, edited)
	compiled, err := plugin.Compile(component, prepared, resolved, flow.NewDescriptors(inputs[selectedIndex]))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
	packets := mustMP4Allocator(t, 8)
	collector := &muxWriteCollector{}
	for _, sample := range movieSamples(t, data, inspected, selectedIndex) {
		input := muxSample(t, data, sample, packets)
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &input), collector); err != nil {
			t.Fatal(err)
		}
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	return applyMuxWrites(t, collector.items)
}

func muxSpyResolver(t testing.TB) (metadata.Resolver, *int) {
	return muxSpyResolverWithPayload(t, metadata.NewBlob(ilstMediaType, nil))
}

func muxSpyResolverWithPayload(t testing.TB, payload metadata.Blob) (metadata.Resolver, *int) {
	t.Helper()
	calls := new(int)
	component := plugin.NewComponent[muxSpyEncodingID](plugin.Descriptor{DisplayName: "test mux ilst binding"}, configurationSchema(), metadata.WithEncoding(
		func(ctx metadata.ParseContext) (metadata.Document, error) {
			builder := metadata.NewBuilder(ctx.Scope())
			builder.AddBlock(metadata.NewSourceBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
			return builder.Build()
		},
		func(metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
			*calls = *calls + 1
			return payload, nil, nil
		},
		muxSpyKey.Erased(),
	))
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{IlstCarrier(): component}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver, calls
}

func descriptorsToInputs(values []flow.PortDescriptor[stream.Descriptor]) []stream.Descriptor {
	result := make([]stream.Descriptor, 0, len(values))
	for _, value := range values {
		result = append(result, value.Descriptor())
	}
	return result
}

func muxEditedTitle(t testing.TB, source metadata.Document, title string) metadata.Document {
	t.Helper()
	builder := metadata.NewBuilder(source.Scope())
	for _, block := range source.Blocks() {
		builder.AddBlock(block)
	}
	for _, entry := range source.Entries() {
		value, ok := entry.Value().(string)
		if !ok || entry.Key() != tag.Title().ID() {
			continue
		}
		if value == title {
			metadata.Add(builder, tag.Title(), value, entry.Origin())
		} else {
			metadata.Add(builder, tag.Title(), title, entry.Origin())
		}
	}
	result, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func muxMetadataInputs(t testing.TB, inspected movie, document metadata.Document) []flow.PortDescriptor[stream.Descriptor] {
	t.Helper()
	result := make([]flow.PortDescriptor[stream.Descriptor], 0, len(inspected.tracks))
	for _, track := range inspected.tracks {
		properties, err := codec.WithTag(property.New(), SampleEntryTag(string(track.codec[:])))
		if err != nil {
			t.Fatal(err)
		}
		input := stream.MustDescriptor(trackStreamID(track.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(track.timeScale)), properties).WithMetadata(document)
		result = append(result, flow.Describe("packets", input))
	}
	return result
}
