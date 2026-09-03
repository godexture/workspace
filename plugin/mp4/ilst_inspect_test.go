package mp4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/access"
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

type ilstInspectForeignCarrierID struct{}
type ilstInspectOverrideID struct{}
type ilstInspectOverrideKeyID struct{}

var ilstInspectOverrideKey = key.Define[ilstInspectOverrideKeyID, string]()

func ilstInspectOverrideComponent() plugin.Component {
	return plugin.NewComponent[ilstInspectOverrideID](plugin.Descriptor{DisplayName: "test ilst binding override"}, configurationSchema(),
		metadata.WithEncoding(
			func(ctx metadata.ParseContext) (metadata.Document, error) {
				builder := metadata.NewBuilder(ctx.Scope())
				builder.AddBlock(metadata.NewSourceBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
				builder.AddBlock(metadata.NewRawBlock("override/raw", ctx.Carrier(), ctx.Encoding(), metadata.NewBlob("application/x-test-ilst-item", []byte("opaque"))))
				metadata.Add(builder, ilstInspectOverrideKey, "override", metadata.Origin{Carrier: ctx.Carrier(), Encoding: ctx.Encoding(), Block: ctx.Block(), Native: "custom"})
				return builder.Build()
			},
			func(metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
				return metadata.NewBlob(ilstMediaType, nil), nil, nil
			},
			ilstInspectOverrideKey.Erased(),
		),
	)
}

func TestMP4InspectRetainsResolvedIlstDocument(t *testing.T) {
	unknown := ilstTestAtom(ilstType{'-', '-', '-', '-'}, []byte{0xff, 0, 1, 2, 3})
	items := [][]byte{
		unknown,
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))),
		ilstTestItem(ilstDate, ilstTestData(ilstDataTypeUTF8, 0, []byte("1985-01-02"))),
		ilstTestItem(ilstTrack, ilstTestData(0, 0, []byte{0, 0, 0, 2, 0, 12, 0, 0})),
		ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, []byte{1, 2, 3})),
	}
	data := ilstMovieFixture("mdir", items...)
	inspected := inspectMovieWithIlst(t, data)

	document := inspected.metadata
	if document.Scope() != metadata.AssetScope || len(document.Blocks()) != 2 {
		t.Fatalf("inspected metadata = scope %s blocks %#v", document.Scope(), document.Blocks())
	}
	source, ok := document.Block(inspected.ilst.block)
	if !ok || !source.Source() || source.Carrier() != IlstCarrier() || source.Encoding() != IlstEncodingIdentity() {
		t.Fatalf("ilst source block = %#v/%v", source, ok)
	}
	opaque, ok := document.Block(ilstItemBlockID(inspected.ilst.block, 0))
	if !ok || opaque.Source() || opaque.Carrier() != IlstCarrier() || opaque.Encoding() != IlstEncodingIdentity() || !opaque.Payload().Equal(metadata.NewBlob(ilstItemMediaType, unknown)) {
		t.Fatalf("ilst opaque block = %#v/%v", opaque, ok)
	}
	sourceBytes := source.Payload().AppendTo(nil)
	opaqueBytes := opaque.Payload().AppendTo(nil)
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "Title" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	if date, ok := metadata.First(document, tag.Date()); !ok || date.ToISOString() != "1985-01-02" {
		t.Fatalf("date = %q/%v", date.ToISOString(), ok)
	}
	if number, ok := metadata.First(document, tag.TrackNumber()); !ok || number != 2 {
		t.Fatalf("track number = %d/%v", number, ok)
	}
	if total, ok := metadata.First(document, tag.TotalTracks()); !ok || total != 12 {
		t.Fatalf("track total = %d/%v", total, ok)
	}
	pictures := metadata.Values(document, tag.Picture())
	if len(pictures) != 1 || pictures[0].MediaType != "image/jpeg" || !bytes.Equal(pictures[0].Data.AppendTo(nil), []byte{1, 2, 3}) {
		t.Fatalf("pictures = %#v", pictures)
	}
	for _, entry := range document.Entries() {
		origin := entry.Origin()
		if origin.Carrier != IlstCarrier() || origin.Encoding != IlstEncodingIdentity() || origin.Block != inspected.ilst.block || origin.Native == "" {
			t.Fatalf("retained source origin = %#v", origin)
		}
	}
	if !inspected.ilst.valid() || inspected.ilst.block != ilstSourceBlockID(inspected.ilst.ilst) {
		t.Fatalf("ilst source layout = %#v", inspected.ilst)
	}

	for index := range data {
		data[index] = 0
	}
	if !bytes.Equal(source.Payload().AppendTo(nil), sourceBytes) || !bytes.Equal(opaque.Payload().AppendTo(nil), opaqueBytes) {
		t.Fatal("retained ilst blocks reference the mutable source slice")
	}
	if got := metadata.Values(document, tag.Picture())[0].Data.AppendTo(nil); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("artwork retained a source buffer: %x", got)
	}

	_, compiled := compileMP4(t, inspected)
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok || len(outputs.At("packets")) != len(inspected.tracks) {
		t.Fatalf("demux outputs = %#v/%v", outputs, ok)
	}
	for _, output := range outputs.At("packets") {
		if !sameIlstMuxDocument(output.Metadata(), document) {
			t.Fatalf("track %q metadata = %#v, want resolved asset document", output.ID(), output.Metadata())
		}
	}
}

func TestMP4InspectUsesResolvedIlstBindingDocument(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{IlstCarrier(): ilstInspectOverrideComponent()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspectMP4(newMP4InspectContext(t, data, prepared, 1<<20, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	inspected, ok := inspectionValue[movie](inspection)
	if !ok {
		t.Fatal("MP4 inspection did not carry movie")
	}
	document := inspected.metadata
	if document.Scope() != metadata.AssetScope || document.Len() != 1 || len(document.Blocks()) != 2 {
		t.Fatalf("override document = %#v", document)
	}
	entry := document.Entries()[0]
	if entry.Key() != ilstInspectOverrideKey.ID() || entry.Value() != "override" {
		t.Fatalf("override entry = %#v", entry)
	}
	origin := entry.Origin()
	if origin.Carrier != IlstCarrier() || origin.Encoding != ilstInspectOverrideComponent().Identity() || origin.Block != inspected.ilst.block || origin.Native != "custom" {
		t.Fatalf("override origin = %#v", origin)
	}
	source, ok := document.Block(inspected.ilst.block)
	if !ok || !source.Source() || source.Payload().MediaType() != ilstMediaType {
		t.Fatalf("override source block = %#v/%v", source, ok)
	}
	opaque, ok := document.Block("override/raw")
	if !ok || opaque.Source() || !opaque.Payload().Equal(metadata.NewBlob("application/x-test-ilst-item", []byte("opaque"))) {
		t.Fatalf("override opaque block = %#v/%v", opaque, ok)
	}
	sourceBytes := source.Payload().AppendTo(nil)
	for index := range data {
		data[index] = 0
	}
	if !bytes.Equal(source.Payload().AppendTo(nil), sourceBytes) {
		t.Fatal("override source block references mutable input")
	}
	_, compiled := compileMP4(t, inspected)
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok || outputs.Len() != len(inspected.tracks) {
		t.Fatalf("override demux outputs = %#v/%v", outputs, ok)
	}
	for _, output := range outputs.At("packets") {
		if !sameIlstMuxDocument(output.Metadata(), document) {
			t.Fatalf("track %q metadata = %#v, want override document", output.ID(), output.Metadata())
		}
	}
}

func TestMP4IlstMetadataAllowsUnchangedMuxAndRejectsEdit(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	inspected := inspectMovieWithIlst(t, data)
	_, demux := compileMP4(t, inspected)
	outputs, ok := plugin.OutputsOf[stream.Descriptor](demux)
	if !ok {
		t.Fatal("demux output descriptors are absent")
	}
	inputs := outputs.At("packets")
	if _, err := compileMux(inputs, inspected); err != nil {
		t.Fatalf("unchanged metadata mux = %v", err)
	}

	changed := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(changed, tag.Title(), "Edited", metadata.Origin{})
	edited, err := changed.Build()
	if err != nil {
		t.Fatal(err)
	}
	inputs = append([]stream.Descriptor(nil), inputs...)
	inputs[0] = inputs[0].WithMetadata(edited)
	if _, err := compileMux(inputs, inspected); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("edited metadata mux error = %v, want unsupported", err)
	}
	foreign := carrier.Define[ilstInspectForeignCarrierID]()
	for _, block := range []metadata.RawBlock{
		metadata.NewRawBlock("foreign", foreign, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, []byte("opaque"))),
		metadata.NewSourceBlock("foreign", foreign, IlstEncodingIdentity(), metadata.NewBlob(ilstMediaType, []byte("source"))),
	} {
		builder := metadata.NewBuilder(metadata.AssetScope)
		builder.AddBlock(block)
		metadata.Add(builder, tag.Title(), "Title", metadata.Origin{})
		foreignDocument, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		inputs[0] = outputs.At("packets")[0].WithMetadata(foreignDocument)
		if _, err := compileMux(inputs, inspected); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("foreign %t block mux error = %v, want unsupported", block.Source(), err)
		}
	}

	component, compiled := compileMP4Mux(t, inspected)
	buffers := mustMP4Allocator(t, 1<<20)
	packets := mustMP4Allocator(t, 8)
	journal, err := scratchMP4Journal(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
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
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, data) {
		t.Fatalf("unchanged ilst remux changed source\n got %x\nwant %x", encoded, data)
	}
}

func TestMP4IlstMetadataUnavailableKeepsRemuxAvailable(t *testing.T) {
	valid := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	malformedMeta := fixtureContainer("udta", fixtureBox("meta", []byte{0, 0, 0, 0, 1}))
	duplicate := ilstMetaFixture("mdir", fixtureBox("ilst", valid), fixtureBox("ilst", valid))
	quickTime := fixtureContainer("udta", fixtureBox("meta", fixtureBox("hdlr", make([]byte, 12))))
	shortHandlerPayload := make([]byte, 12)
	copy(shortHandlerPayload[8:], "mdir")
	shortHandler := fixtureContainer("udta", fixtureBox("meta", fixtureFullBox(0, 0, append(fixtureBox("hdlr", shortHandlerPayload), fixtureBox("ilst", valid)...))))
	for _, test := range []struct {
		name          string
		extra         []byte
		memory        resource.Bytes
		read          resource.Bytes
		wantOffsetIdx bool
	}{
		{name: "absent"},
		{name: "non-itunes", extra: ilstMetaFixture("soun", fixtureBox("ilst", valid))},
		{name: "quicktime", extra: quickTime, wantOffsetIdx: true},
		{name: "short handler", extra: shortHandler},
		{name: "malformed", extra: malformedMeta},
		{name: "malformed ilst", extra: ilstMetaFixture("mdir", fixtureBox("ilst", []byte{0, 0, 0, 8}))},
		{name: "duplicate", extra: duplicate},
		{name: "unwalkable udta", extra: fixtureContainer("udta", []byte{0, 0, 0, 4}), wantOffsetIdx: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := ilstMovieFixtureWithExtra(test.extra)
			memory := test.memory
			if memory == 0 {
				memory = 1 << 20
			}
			read := test.read
			if read == 0 {
				read = 1 << 20
			}
			inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), read, memory, ilstTestResolver(t, IlstCarrier()))
			if err != nil {
				t.Fatalf("optional metadata parse = %v", err)
			}
			if inspected.metadata.Scope().Valid() || inspected.ilst.valid() {
				t.Fatalf("optional metadata = %#v %#v, want unavailable", inspected.metadata, inspected.ilst)
			}
			if test.wantOffsetIdx && !inspected.offsetIndex {
				t.Fatalf("unavailable metadata lost offset index: %#v", inspected)
			}
			if _, compiled := compileMP4Mux(t, inspected); !compiled.Valid() {
				t.Fatal("unavailable metadata prevented same-format remux")
			}
		})
	}
}

func TestMP4IlstPayloadBudgetBoundary(t *testing.T) {
	artwork := bytes.Repeat([]byte{0x5a}, 64<<10)
	payload := ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, artwork))
	data := ilstMovieFixture("mdir", payload)
	trackMemory := trackBudgetBytes * 2
	for _, test := range []struct {
		name          string
		memory        resource.Bytes
		wantAvailable bool
	}{
		{name: "payload fits", memory: resource.Bytes(trackMemory + uint64(len(payload))), wantAvailable: true},
		{name: "one byte short", memory: resource.Bytes(trackMemory + uint64(len(payload)) - 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 1<<20, test.memory, ilstTestResolver(t, IlstCarrier()))
			if !test.wantAvailable {
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("one-byte-short metadata failure = %v, want unsupported", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("boundary metadata parse = %v", err)
			}
			available := inspected.metadata.Scope().Valid()
			if available != test.wantAvailable || available != inspected.ilst.valid() {
				t.Fatalf("boundary metadata availability = %v/%v, want %v", available, inspected.ilst.valid(), test.wantAvailable)
			}
			if available {
				pictures := metadata.Values(inspected.metadata, tag.Picture())
				if len(pictures) != 1 || pictures[0].Data.Len() != len(artwork) {
					t.Fatalf("boundary artwork = %#v, want %d bytes", pictures, len(artwork))
				}
			}
			if _, compiled := compileMP4Mux(t, inspected); !compiled.Valid() {
				t.Fatal("payload boundary prevented same-format remux")
			}
		})
	}
}

func TestMP4IlstPayloadBudgetFailureIsExplicit(t *testing.T) {
	artwork := bytes.Repeat([]byte{0x5a}, 64<<10)
	payload := ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, artwork))
	data := ilstMovieFixture("mdir", payload)
	for _, test := range []struct {
		name   string
		read   resource.Bytes
		memory resource.Bytes
	}{
		{name: "retained memory", memory: resource.Bytes(trackBudgetBytes*2 + uint64(len(payload)) - 1)},
		{name: "inspect bytes", read: 4096, memory: 1 << 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			read, memory := test.read, test.memory
			if read == 0 {
				read = 1 << 20
			}
			if memory == 0 {
				memory = 1 << 20
			}
			_, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), read, memory, ilstTestResolver(t, IlstCarrier()))
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("metadata budget failure = %v, want unsupported", err)
			}
		})
	}
}

func TestMP4IlstDuplicateWithoutIlocAllowsSubsetRemux(t *testing.T) {
	valid := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	data := ilstMovieFixtureWithExtra(ilstMetaFixture("mdir", fixtureBox("ilst", valid), fixtureBox("ilst", valid)))
	inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if err != nil {
		t.Fatalf("duplicate metadata parse = %v", err)
	}
	if inspected.metadata.Scope().Valid() || inspected.offsetIndex {
		t.Fatalf("duplicate metadata = %#v, want unavailable without offset index", inspected)
	}
	if encoded := runSubsetMux(t, data, 0); len(encoded) == 0 {
		t.Fatal("duplicate metadata prevented subset remux")
	}
}

func TestMP4IlstMetadataRequiresResolvedBinding(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	resolver, err := metadata.NewResolver(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20, resolver)
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "metadata.binding-missing" && item.Detail["carrier"] == IlstCarrier().String() {
			return
		}
	}
	t.Fatalf("missing ilst binding diagnostic = %v", err)
}

func TestMP4InspectIlstRequiresPreparedResolver(t *testing.T) {
	data := ilstMovieFixture("mdir", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))))
	_, err := inspectMP4(newMP4InspectContext(t, data, plugin.CompileContext{}, 1<<20, 1<<20))
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "metadata.binding-missing" && item.Detail["carrier"] == IlstCarrier().String() {
			return
		}
	}
	t.Fatalf("unprepared ilst resolver diagnostic = %v", err)
}

func TestMP4IlstMetadataPropagatesSourceReadFailure(t *testing.T) {
	item := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	data := ilstMovieFixture("mdir", item)
	start := bytes.Index(data, item)
	if start < 0 {
		t.Fatal("ilst fixture does not contain item payload")
	}
	failure := errors.New("ilst source stopped answering")
	reader := ilstFailingRandom{Random: memoryRandom(data), start: int64(start), err: failure}
	_, err := parseMovieWithMetadata(t.Context(), reader, uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if !errors.Is(err, failure) {
		t.Fatalf("source read failure = %v, want %v", err, failure)
	}
}

func TestMP4IlstMetadataPropagatesInvalidSourceRead(t *testing.T) {
	item := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	data := ilstMovieFixture("mdir", item)
	start := bytes.Index(data, item)
	if start < 0 {
		t.Fatal("ilst fixture does not contain item payload")
	}
	reader := ilstFailingRandom{Random: memoryRandom(data), start: int64(start), err: access.ErrInvalidRead}
	_, err := parseMovieWithMetadata(t.Context(), reader, uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if !errors.Is(err, access.ErrInvalidRead) {
		t.Fatalf("invalid source read error = %v, want %v", err, access.ErrInvalidRead)
	}
}

func TestMP4IlstMetadataPropagatesTruncatedSourceRead(t *testing.T) {
	item := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	data := ilstMovieFixture("mdir", item)
	start := bytes.Index(data, item)
	if start < 0 {
		t.Fatal("ilst fixture does not contain item payload")
	}
	reader := ilstFailingRandom{Random: memoryRandom(data), start: int64(start), err: io.EOF}
	_, err := parseMovieWithMetadata(t.Context(), reader, uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated metadata source error = %v, want ErrTruncated", err)
	}
}

func TestMP4IlstMetadataDoesNotSpendRequiredReadBudget(t *testing.T) {
	base := ilstMovieFixtureWithExtra(nil)
	counter := &ilstCountingRandom{Random: memoryRandom(base)}
	if _, err := parseMovie(t.Context(), counter, uint64(len(base)), 1<<20, 1<<20); err != nil {
		t.Fatal(err)
	}
	children := make([][]byte, 0, 129)
	for range 128 {
		children = append(children, fixtureBox("free", nil))
	}
	children = append(children, fixtureBox("ilst", ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))))
	data := ilstMovieFixtureWithExtra(ilstMetaFixture("mdir", children...))
	read := resource.Bytes(counter.read + 16)
	inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), read, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if err != nil {
		t.Fatalf("required MP4 inspection lost its read budget: %v", err)
	}
	if !inspected.valid() || inspected.metadata.Scope().Valid() {
		t.Fatalf("tight-budget inspection = %#v", inspected)
	}
}

func TestMP4IlstMetadataKeepsDirectIlocWhenUnavailable(t *testing.T) {
	valid := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	handler := make([]byte, 24)
	copy(handler[8:], "mdir")
	unwalkable := fixtureContainer("udta", fixtureBox("meta", append(fixtureFullBox(0, 0, fixtureBox("hdlr", handler)), 0, 0, 0, 8)))
	for _, test := range []struct {
		name  string
		extra []byte
	}{
		{name: "wrong handler", extra: ilstMetaFixture("soun", fixtureBox("iloc", nil), fixtureBox("ilst", valid))},
		{name: "malformed payload", extra: ilstMetaFixture("mdir", fixtureBox("iloc", nil), fixtureBox("ilst", []byte{0, 0, 0, 8}))},
		{name: "duplicate target", extra: ilstMetaFixture("mdir", fixtureBox("iloc", nil), fixtureBox("ilst", valid), fixtureBox("ilst", valid))},
		{name: "unwalkable fullbox", extra: unwalkable},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := ilstMovieFixtureWithExtra(test.extra)
			inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
			if err != nil {
				t.Fatalf("optional metadata = %v", err)
			}
			if inspected.metadata.Scope().Valid() || !inspected.offsetIndex {
				t.Fatalf("unavailable metadata lost direct iloc constraint: %#v", inspected)
			}
			if _, compiled := compileMP4Mux(t, inspected); !compiled.Valid() {
				t.Fatal("unavailable metadata prevented unchanged remux")
			}
		})
	}
}

func TestMP4InspectAcceptedIlstMetaRetainsDirectIlocConstraint(t *testing.T) {
	item := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	data := ilstMovieFixtureWithExtra(ilstMetaFixture("mdir", fixtureBox("iloc", nil), fixtureBox("ilst", item)))
	inspected := inspectMovieWithIlst(t, data)
	if !inspected.ilst.valid() || !inspected.offsetIndex {
		t.Fatalf("accepted ilst meta did not retain iloc constraint: %#v", inspected)
	}
}

func TestMP4IlstOptionalTraversalBeforeLateIlocFailsClosed(t *testing.T) {
	valid := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	malformed := fixtureContainer("udta", fixtureBox("meta", []byte{0}))
	late := ilstMetaFixture("mdir", fixtureBox("iloc", nil), fixtureBox("ilst", valid))
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}, nil, [][]byte{malformed, late})
	inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 1<<20, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if err != nil {
		t.Fatalf("optional malformed metadata = %v", err)
	}
	if inspected.metadata.Scope().Valid() || !inspected.offsetIndex {
		t.Fatalf("optional traversal before late iloc = %#v, want unavailable with offset index", inspected)
	}
	if _, compiled := compileMP4Mux(t, inspected); !compiled.Valid() {
		t.Fatal("unavailable metadata prevented unchanged remux")
	}
	selected := inspected.tracks[1]
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(selected.codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor(trackStreamID(selected.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(selected.timeScale)), properties)
	if _, err := compileMux([]stream.Descriptor{input}, inspected); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("late iloc subset mux error = %v, want unsupported", err)
	}
}

func TestMP4IlstReadBudgetBeforeLateIlocFailsClosed(t *testing.T) {
	valid := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	children := make([][]byte, 0, 600)
	for range 600 {
		children = append(children, fixtureBox("free", nil))
	}
	children = append(children, fixtureBox("iloc", nil), fixtureBox("ilst", valid))
	data := ilstMovieFixtureWithExtra(ilstMetaFixture("mdir", children...))
	inspected, err := parseMovieWithMetadata(t.Context(), memoryRandom(data), uint64(len(data)), 4096, 1<<20, ilstTestResolver(t, IlstCarrier()))
	if err != nil {
		t.Fatalf("optional read-budget metadata = %v", err)
	}
	if inspected.metadata.Scope().Valid() || !inspected.offsetIndex {
		t.Fatalf("read-budget traversal before late iloc = %#v, want unavailable with offset index", inspected)
	}
	if _, compiled := compileMP4Mux(t, inspected); !compiled.Valid() {
		t.Fatal("unavailable metadata prevented unchanged remux")
	}
	selected := inspected.tracks[1]
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(selected.codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor(trackStreamID(selected.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(selected.timeScale)), properties)
	if _, err := compileMux([]stream.Descriptor{input}, inspected); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("read-budget late iloc subset mux error = %v, want unsupported", err)
	}
}

func TestMP4IlstDocumentTreatsAbsentAndEmptyAsEqual(t *testing.T) {
	empty, err := metadata.NewBuilder(metadata.AssetScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	if !sameIlstMuxDocument(metadata.Document{}, empty) || !sameIlstMuxDocument(empty, metadata.Document{}) {
		t.Fatal("absent and empty metadata documents are not equivalent")
	}
}

func inspectMovieWithIlst(t testing.TB, data []byte) movie {
	t.Helper()
	resolver := ilstTestResolver(t, IlstCarrier())
	prepared, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspectMP4(newMP4InspectContext(t, data, prepared, 1<<20, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := inspectionValue[movie](inspection)
	if !ok {
		t.Fatal("MP4 inspection did not carry movie")
	}
	return value
}

func newMP4InspectContext(t testing.TB, data []byte, prepared plugin.CompileContext, read, memory resource.Bytes) mediaformat.InspectContext {
	t.Helper()
	return mediaformat.NewInspectContext(t.Context(), movieSourceOpening(t, data), prepared, read, memory)
}

func ilstMovieFixture(handler string, items ...[]byte) []byte {
	return ilstMovieFixtureWithExtra(ilstMetaFixture(handler, fixtureBox("ilst", bytes.Join(items, nil))))
}

func ilstMovieFixtureWithExtra(extra []byte) []byte {
	tracks := []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}
	var extras [][]byte
	if len(extra) != 0 {
		extras = append(extras, extra)
	}
	return fixtureMovie(false, "isom", []string{"iso2"}, tracks, nil, extras)
}

func ilstMetaFixture(handler string, children ...[]byte) []byte {
	handlerPayload := make([]byte, 24)
	copy(handlerPayload[8:], handler)
	values := make([][]byte, 0, len(children)+1)
	values = append(values, fixtureBox("hdlr", handlerPayload))
	values = append(values, children...)
	return fixtureContainer("udta", fixtureBox("meta", fixtureFullBox(0, 0, bytes.Join(values, nil))))
}

func scratchMP4Journal(compiled plugin.Compilation) (*scratch.Journal, error) {
	return scratch.Open(compiled.Scratch())
}

type ilstFailingRandom struct {
	access.Random
	start int64
	err   error
}

type ilstCountingRandom struct {
	access.Random
	read uint64
}

func (r *ilstCountingRandom) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	count, err := r.Random.ReadAt(ctx, destination, offset)
	if count > 0 {
		r.read += uint64(count)
	}
	return count, err
}

func (r ilstFailingRandom) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if offset >= r.start {
		return 0, r.err
	}
	return r.Random.ReadAt(ctx, destination, offset)
}
