package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type ilstTestCarrierID struct{}
type ilstForeignCarrierID struct{}

func TestIlstParsesSemanticsAndPreservesSource(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	payload := bytes.Join([][]byte{
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))),
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Second"))),
		ilstTestItem(ilstArt, ilstTestData(ilstDataTypeUTF8, 0, nil)),
		ilstTestItem(ilstDate, ilstTestData(ilstDataTypeUTF8, 0, []byte("1985-01-02"))),
		ilstTestItem(ilstTrack, ilstTestData(0, 0, []byte{0, 0, 0, 2, 0, 12, 0, 0})),
		ilstTestItem(ilstDisc, ilstTestData(0, 0, []byte{0, 0, 0, 1, 0, 2, 0, 0})),
		ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, []byte{1, 2})),
		ilstTestItem(ilstCover, ilstTestData(ilstDataTypePNG, 0, []byte{3, 4})),
	}, nil)
	document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "Title" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	if !reflect.DeepEqual(metadata.Values(document, tag.Title()), []string{"Title", "Second"}) {
		t.Fatalf("titles = %#v", metadata.Values(document, tag.Title()))
	}
	if origin := document.Entries()[0].Origin(); origin.Carrier != slot || origin.Encoding != IlstEncodingIdentity() || origin.Block != "ilst" || origin.Native != ilstAtomString(ilstName) {
		t.Fatalf("title origin = %#v", origin)
	}
	if artist, ok := metadata.First(document, tag.Artist()); !ok || artist != "" {
		t.Fatalf("artist = %q/%v", artist, ok)
	}
	if date, ok := metadata.First(document, tag.Date()); !ok || date.ToISOString() != "1985-01-02" {
		t.Fatalf("date = %q/%v", date.ToISOString(), ok)
	}
	if !reflect.DeepEqual(metadata.Values(document, tag.TrackNumber()), []int64{2}) || !reflect.DeepEqual(metadata.Values(document, tag.TotalTracks()), []int64{12}) || !reflect.DeepEqual(metadata.Values(document, tag.DiscNumber()), []int64{1}) || !reflect.DeepEqual(metadata.Values(document, tag.TotalDiscs()), []int64{2}) {
		t.Fatalf("ordinals = %#v %#v %#v %#v", metadata.Values(document, tag.TrackNumber()), metadata.Values(document, tag.TotalTracks()), metadata.Values(document, tag.DiscNumber()), metadata.Values(document, tag.TotalDiscs()))
	}
	pictures := metadata.Values(document, tag.Picture())
	if len(pictures) != 2 || pictures[0].MediaType != "image/jpeg" || !bytes.Equal(pictures[0].Data.AppendTo(nil), []byte{1, 2}) || pictures[1].MediaType != "image/png" || !bytes.Equal(pictures[1].Data.AppendTo(nil), []byte{3, 4}) {
		t.Fatalf("pictures = %#v", pictures)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstSourceReuseIgnoresForeignSourceAnchor(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	artwork := bytes.Repeat([]byte{0x5a}, 1<<20)
	payload := bytes.Join([][]byte{
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title"))),
		ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, artwork)),
	}, nil)
	sourcePayload := metadata.NewBlob(ilstMediaType, payload)
	parsed, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, sourcePayload)
	if err != nil {
		t.Fatal(err)
	}
	foreign := carrier.Define[ilstForeignCarrierID]()
	builder := parsed.Edit()
	builder.AddBlock(metadata.NewSourceBlock("foreign/source", foreign, IlstEncodingIdentity(), metadata.NewBlob("application/octet-stream", bytes.Repeat([]byte{1}, 1<<20))))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || encoded != sourcePayload {
		t.Fatalf("source reuse with foreign anchor = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstSourceReuseRejectsOwnedMutation(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	item := ilstTestItem(ilstType{'-', '-', '-', '-'}, []byte{1})
	sourcePayload := metadata.NewBlob(ilstMediaType, item)
	parsed, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, sourcePayload)
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	for _, block := range parsed.Blocks() {
		if !block.Source() {
			block = metadata.NewRawBlock(block.ID(), block.Carrier(), block.Encoding(), metadata.NewBlob(ilstItemMediaType, ilstTestAtom(ilstType{'-', '-', '-', '-'}, []byte{2})))
		}
		builder.AddBlock(block)
	}
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || encoded.Equal(sourcePayload) {
		t.Fatalf("owned opaque mutation reused source = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstSourceReuseRejectsSourceMutation(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	item := ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("Title")))
	sourcePayload := metadata.NewBlob(ilstMediaType, item)
	parsed, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, sourcePayload)
	if err != nil {
		t.Fatal(err)
	}
	mutated := sourcePayload.AppendTo(nil)
	start := bytes.Index(mutated, []byte("Title"))
	if start < 0 {
		t.Fatal("source fixture does not contain title")
	}
	copy(mutated[start:start+len("Title")], []byte("Other"))
	mutatedPayload := metadata.NewBlob(ilstMediaType, mutated)
	builder := metadata.NewBuilder(metadata.AssetScope)
	for _, block := range parsed.Blocks() {
		if block.Source() {
			block = metadata.NewSourceBlock(block.ID(), block.Carrier(), block.Encoding(), mutatedPayload)
		}
		builder.AddBlock(block)
	}
	entry := parsed.Entries()[0]
	metadata.Add(builder, tag.Title(), entry.Value().(string), entry.Origin())
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || encoded.Equal(mutatedPayload) {
		t.Fatalf("source mutation reused source = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstMuxDocumentComparesEveryField(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	source := metadata.NewSourceBlock("source", slot, IlstEncodingIdentity(), metadata.NewBlob(ilstMediaType, []byte("source")))
	opaque := metadata.NewRawBlock("opaque", slot, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, []byte("opaque")))
	build := func(blocks ...metadata.RawBlock) metadata.Document {
		t.Helper()
		builder := metadata.NewBuilder(metadata.AssetScope)
		for _, block := range blocks {
			builder.AddBlock(block)
		}
		document, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	base := build(source, opaque)
	foreign := carrier.Define[ilstForeignCarrierID]()
	mutations := []struct {
		name  string
		value metadata.Document
	}{
		{name: "source flag", value: build(metadata.NewRawBlock("source", slot, IlstEncodingIdentity(), source.Payload()), opaque)},
		{name: "block order", value: build(opaque, source)},
		{name: "carrier", value: build(source, metadata.NewRawBlock("opaque", foreign, IlstEncodingIdentity(), opaque.Payload()))},
		{name: "encoding", value: build(source, metadata.NewRawBlock("opaque", slot, plugin.Identity{}, opaque.Payload()))},
		{name: "payload", value: build(source, metadata.NewRawBlock("opaque", slot, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, []byte("changed"))))},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if sameIlstMuxDocument(base, mutation.value) {
				t.Fatalf("mux comparator accepted %s mutation", mutation.name)
			}
		})
	}

	origin := metadata.Origin{Carrier: slot, Encoding: IlstEncodingIdentity(), Block: source.ID(), Native: "source"}
	withEntry := metadata.NewBuilder(metadata.AssetScope)
	withEntry.AddBlock(source)
	metadata.Add(withEntry, tag.Title(), "title", origin)
	entryDocument, err := withEntry.Build()
	if err != nil {
		t.Fatal(err)
	}
	changedOrigin := metadata.NewBuilder(metadata.AssetScope)
	changedOrigin.AddBlock(source)
	metadata.Add(changedOrigin, tag.Title(), "title", metadata.Origin{Carrier: slot, Encoding: IlstEncodingIdentity(), Block: source.ID(), Native: "changed"})
	changedOriginDocument, err := changedOrigin.Build()
	if err != nil {
		t.Fatal(err)
	}
	if sameIlstMuxDocument(entryDocument, changedOriginDocument) {
		t.Fatal("mux comparator accepted origin mutation")
	}
}

func TestIlstParsesLargeDataAtom(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	data := binary.BigEndian.AppendUint32(nil, ilstDataTypeUTF8)
	data = binary.BigEndian.AppendUint32(data, 0)
	data = append(data, "large"...)
	payload := ilstTestItem(ilstName, ilstTestLargeAtom(ilstData, data))
	document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "large" {
		t.Fatalf("large data title = %q/%v", title, ok)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || !encoded.Equal(metadata.NewBlob(ilstMediaType, payload)) {
		t.Fatalf("large data source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstTextVocabulary(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	values := []struct {
		typeID ilstType
		value  string
		read   func(metadata.Document) (string, bool)
	}{
		{ilstName, "Title", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Title()) }},
		{ilstArt, "Artist", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Artist()) }},
		{ilstAlbum, "Album", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Album()) }},
		{ilstComposer, "Composer", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Composer()) }},
		{ilstGenre, "Genre", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Genre()) }},
		{ilstComment, "Comment", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Comment()) }},
		{ilstLyrics, "Lyrics", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Lyrics()) }},
		{ilstCopyright, "Copyright", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Copyright()) }},
		{ilstEncoder, "Encoder", func(document metadata.Document) (string, bool) { return metadata.First(document, tag.Encoder()) }},
	}
	payload := make([]byte, 0)
	for _, value := range values {
		payload = append(payload, ilstTestItem(value.typeID, ilstTestData(ilstDataTypeUTF8, 0, []byte(value.value)))...)
	}
	document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		got, ok := value.read(document)
		if !ok || got != value.value {
			t.Fatalf("%s = %q/%v", ilstAtomString(value.typeID), got, ok)
		}
	}
}

func TestIlstCanonicalOrdinalAndPictureLosses(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.TrackNumber(), int64(2), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(12), metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/jpeg", []byte{1}), MediaType: "image/jpeg", Type: tag.ArtworkFrontCover, Description: "cover", Width: 100}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{2}), MediaType: "image/png", Type: tag.ArtworkFrontCover}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 2 || reports[0].Loss.Kind != loss.Truncated || reports[1].Loss.Kind != loss.Folded {
		t.Fatalf("canonical reports %#v, error %v", reports, err)
	}
	items, err := ilstScan(encoded, 0, encoded.Len())
	if err != nil || len(items) != 2 || items[0].typeID != ilstTrack || items[1].typeID != ilstCover {
		t.Fatalf("canonical items %#v, error %v", items, err)
	}
	track, ok := ilstDataAtom(encoded, mustIlstChild(t, encoded, items[0]))
	if !ok || track.typeCode != 0 || track.locale != 0 || !bytes.Equal(track.value.AppendTo(nil), []byte{0, 0, 0, 2, 0, 12, 0, 0}) {
		t.Fatalf("trkn data = %#v/%v", track, ok)
	}
	cover, ok := ilstDataAtom(encoded, mustIlstChild(t, encoded, items[1]))
	if !ok || cover.typeCode != ilstDataTypeJPEG || !bytes.Equal(cover.value.AppendTo(nil), []byte{1}) {
		t.Fatalf("covr data = %#v/%v", cover, ok)
	}
}

func TestIlstRewriteRetainsOpaquePositionAndUnchangedKnownItems(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	title := ilstTestLargeItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("old")))
	opaque := ilstTestItem(ilstType{'-', '-', '-', '-'}, ilstTestData(1, 0, []byte("opaque")))
	artist := ilstTestLargeItem(ilstArt, ilstTestData(ilstDataTypeUTF8, 0, []byte("artist")))
	payload := bytes.Join([][]byte{title, opaque, artist}, nil)
	parsed, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	blocks := parsed.Blocks()
	builder := metadata.NewBuilder(metadata.AssetScope)
	for _, block := range blocks {
		builder.AddBlock(block)
	}
	origin := metadata.Origin{Carrier: slot, Encoding: IlstEncodingIdentity(), Block: "ilst", Native: ilstAtomString(ilstName)}
	metadata.Add(builder, tag.Title(), "changed", origin)
	artistValue, _ := metadata.First(parsed, tag.Artist())
	artistOrigin := parsed.Entries()[1].Origin()
	metadata.Add(builder, tag.Artist(), artistValue, artistOrigin)
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("rewrite reports %#v, error %v", reports, err)
	}
	items, err := ilstScan(encoded, 0, encoded.Len())
	if err != nil || len(items) != 3 {
		t.Fatalf("rewrite items %#v, error %v", items, err)
	}
	first, _ := ilstAtomBlob(encoded, items[0], ilstItemMediaType)
	second, _ := ilstAtomBlob(encoded, items[1], ilstItemMediaType)
	third, _ := ilstAtomBlob(encoded, items[2], ilstItemMediaType)
	if items[0].typeID != ilstName || !bytes.Equal(second.AppendTo(nil), opaque) || !bytes.Equal(third.AppendTo(nil), artist) {
		t.Fatalf("rewrite order = %x / %x / %x", first.AppendTo(nil), second.AppendTo(nil), third.AppendTo(nil))
	}
	firstData, ok := ilstDataAtom(encoded, mustIlstChild(t, encoded, items[0]))
	if !ok || string(firstData.value.AppendTo(nil)) != "changed" {
		t.Fatalf("changed title = %#v/%v", firstData, ok)
	}
}

func TestIlstRetainsUnrepresentableItemsOpaque(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	for _, test := range []struct {
		name string
		item []byte
	}{
		{name: "nonzero locale", item: ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 1, []byte("title")))},
		{name: "multiple data", item: ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("one")), ilstTestData(ilstDataTypeUTF8, 0, []byte("two")))},
		{name: "unknown child", item: ilstTestItem(ilstName, ilstTestAtom(ilstType{'j', 'u', 'n', 'k'}, nil))},
		{name: "data type mismatch", item: ilstTestItem(ilstName, ilstTestData(0, 0, []byte("title")))},
		{name: "invalid UTF-8", item: ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte{0xff}))},
		{name: "noncanonical date", item: ilstTestItem(ilstDate, ilstTestData(ilstDataTypeUTF8, 0, []byte("1985-01-02T03:04:05Z")))},
		{name: "total only", item: ilstTestItem(ilstTrack, ilstTestData(0, 0, []byte{0, 0, 0, 0, 0, 2, 0, 0}))},
		{name: "reserved ordinal", item: ilstTestItem(ilstTrack, ilstTestData(0, 0, []byte{0, 1, 0, 2, 0, 2, 0, 0}))},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, test.item))
			if err != nil {
				t.Fatal(err)
			}
			if document.Len() != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Payload().MediaType() != ilstItemMediaType {
				t.Fatalf("opaque item = %#v", document)
			}
			builder := document.Edit()
			metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", edited)
			if err != nil || len(reports) != 0 {
				t.Fatalf("opaque rewrite reports %#v, error %v", reports, err)
			}
			items, err := ilstScan(encoded, 0, encoded.Len())
			if err != nil || len(items) != 2 {
				t.Fatalf("opaque rewrite items %#v, error %v", items, err)
			}
			first, _ := ilstAtomBlob(encoded, items[0], ilstItemMediaType)
			if !bytes.Equal(first.AppendTo(nil), test.item) || items[1].typeID != ilstName {
				t.Fatalf("opaque rewrite = %x / %x", first.AppendTo(nil), encoded.AppendTo(nil))
			}
		})
	}
}

func TestIlstRetainsUnknownItemsWithoutDecodingTheirPayload(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	for _, test := range []struct {
		name   string
		typeID ilstType
		body   []byte
	}{
		{name: "unknown fourcc", typeID: ilstType{'-', '-', '-', '-'}, body: []byte{0xff, 0, 1, 2, 3}},
		{name: "top level data", typeID: ilstData, body: []byte{1, 2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := ilstTestAtom(test.typeID, test.body)
			document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, item))
			if err != nil {
				t.Fatal(err)
			}
			if document.Len() != 0 || len(document.Blocks()) != 2 {
				t.Fatalf("unknown item document = %#v", document)
			}
			unchanged, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(unchanged.AppendTo(nil), item) {
				t.Fatalf("unknown source roundtrip = %x, reports %#v, error %v", unchanged.AppendTo(nil), reports, err)
			}
			builder := document.Edit()
			metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", edited)
			if err != nil || len(reports) != 0 {
				t.Fatalf("unknown rewrite reports %#v, error %v", reports, err)
			}
			items, err := ilstScan(encoded, 0, encoded.Len())
			if err != nil || len(items) != 2 {
				t.Fatalf("unknown rewrite items %#v, error %v", items, err)
			}
			first, _ := ilstAtomBlob(encoded, items[0], ilstItemMediaType)
			if !bytes.Equal(first.AppendTo(nil), item) || items[1].typeID != ilstName {
				t.Fatalf("unknown rewrite = %x", encoded.AppendTo(nil))
			}
		})
	}
}

func TestIlstTreatsEmptyCoverAsOpaqueAndDropsFreshValue(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	item := ilstTestItem(ilstCover, ilstTestData(ilstDataTypeJPEG, 0, nil))
	document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, item))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := metadata.First(document, tag.Picture()); ok || document.Len() != 0 || len(document.Blocks()) != 2 {
		t.Fatalf("empty covr document = %#v", document)
	}
	unchanged, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(unchanged.AppendTo(nil), item) {
		t.Fatalf("empty covr source roundtrip = %x, reports %#v, error %v", unchanged.AppendTo(nil), reports, err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", edited)
	if err != nil || len(reports) != 0 || !bytes.HasPrefix(encoded.AppendTo(nil), item) {
		t.Fatalf("empty covr rewrite = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}

	builder = metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/jpeg", nil), MediaType: "image/jpeg", Type: tag.ArtworkFrontCover}, metadata.Origin{})
	fresh, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "ilst", fresh)
	if err != nil || encoded.Len() != 0 || len(reports) != 1 || reports[0].Loss.Kind != loss.Dropped || reports[0].Loss.Detail != "mp4.ilst.picture-unrepresentable" {
		t.Fatalf("empty fresh covr = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestIlstFoldsDuplicateTextAndOrdinalValues(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	payload := bytes.Join([][]byte{
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("first"))),
		ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("second"))),
		ilstTestItem(ilstArt, ilstTestData(ilstDataTypeUTF8, 0, []byte("old"))),
	}, nil)
	parsed, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	for _, block := range parsed.Blocks() {
		builder.AddBlock(block)
	}
	entries := parsed.Entries()
	metadata.Add(builder, tag.Title(), "first", entries[0].Origin())
	metadata.Add(builder, tag.Title(), "second", entries[1].Origin())
	metadata.Add(builder, tag.Artist(), "changed", entries[2].Origin())
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", edited)
	if err != nil || len(reports) != 1 || reports[0].Loss.Kind != loss.Folded || reports[0].Loss.Key != tag.Title().ID() {
		t.Fatalf("text fold reports %#v, error %v", reports, err)
	}
	items, err := ilstScan(encoded, 0, encoded.Len())
	if err != nil || len(items) != 2 || items[0].typeID != ilstName || items[1].typeID != ilstArt {
		t.Fatalf("text fold items %#v, error %v", items, err)
	}

	builder = metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.TrackNumber(), int64(^uint16(0)), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(^uint16(0)), metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(1), metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(-1), metadata.Origin{})
	ordinals, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "ilst", ordinals)
	if err != nil || len(reports) != 2 || reports[0].Loss.Kind != loss.Folded || reports[1].Loss.Kind != loss.Dropped {
		t.Fatalf("ordinal reports %#v, error %v", reports, err)
	}
	items, err = ilstScan(encoded, 0, encoded.Len())
	if err != nil || len(items) != 1 || items[0].typeID != ilstTrack {
		t.Fatalf("ordinal items %#v, error %v", items, err)
	}
	data, ok := ilstDataAtom(encoded, mustIlstChild(t, encoded, items[0]))
	if !ok || !bytes.Equal(data.value.AppendTo(nil), []byte{0, 0, 0xff, 0xff, 0xff, 0xff, 0, 0}) {
		t.Fatalf("ordinal max data = %#v/%v", data, ok)
	}
}

func TestIlstRejectsMalformedAndUnsafeRaw(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	for _, payload := range [][]byte{
		{0, 0, 0, 8},
		{0, 0, 0, 0, 'f', 'r', 'e', 'e'},
		{0, 0, 0, 4, 'f', 'r', 'e', 'e'},
		{0, 0, 0, 16, 'f', 'r', 'e', 'e'},
		{0, 0, 0, 1, 'f', 'r', 'e', 'e'},
		ilstTestDeclaredLarge(ilstName, 8),
		ilstTestDeclaredLarge(ilstName, 64),
		ilstTestItem(ilstName, []byte{0, 0, 0, 0, 'd', 'a', 't', 'a'}),
	} {
		if _, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("malformed payload error = %v for %x", err, payload)
		}
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{})
	builder.AddBlock(metadata.NewRawBlock(ilstItemBlockID("ilst", 0), slot, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("injected"))))))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "ilst", document); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("semantic raw injection error = %v", err)
	}
}

func TestIlstForeignBlocksAndEmptyDocument(t *testing.T) {
	slot := carrier.Define[ilstTestCarrierID]()
	resolver := ilstTestResolver(t, slot)
	empty, err := metadata.NewBuilder(metadata.AssetScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", empty)
	if err != nil || encoded.Len() != 0 || len(reports) != 0 {
		t.Fatalf("empty ilst = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	foreign := carrier.Define[ilstForeignCarrierID]()
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{})
	builder.AddBlock(metadata.NewRawBlock("foreign", foreign, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, ilstTestItem(ilstType{'f', 'r', 'e', 'e'}, nil))))
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "ilst", document); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("foreign opaque error = %v", err)
	}
	builder = metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock("ilst", foreign, IlstEncodingIdentity(), metadata.NewBlob(ilstMediaType, nil)))
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{Carrier: foreign, Encoding: IlstEncodingIdentity(), Block: "ilst", Native: ilstAtomString(ilstName)})
	document, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "ilst", document)
	if err != nil || len(reports) != 0 || encoded.Len() == 0 {
		t.Fatalf("foreign source error = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	builder = metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock("ilst", slot, IlstEncodingIdentity(), metadata.NewBlob(ilstMediaType, nil)))
	builder.AddBlock(metadata.NewRawBlock("foreign", foreign, IlstEncodingIdentity(), metadata.NewBlob(ilstItemMediaType, ilstTestAtom(ilstType{'f', 'r', 'e', 'e'}, nil))))
	document, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "ilst", document); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("foreign opaque with source error = %v, want unsupported", err)
	}
}

func FuzzIlstAcceptedSourceRoundTrip(f *testing.F) {
	f.Add(ilstTestItem(ilstName, ilstTestData(ilstDataTypeUTF8, 0, []byte("title"))))
	f.Add(ilstTestAtom(ilstType{'-', '-', '-', '-'}, []byte{0xff, 0, 1, 2}))
	f.Fuzz(func(t *testing.T, payload []byte) {
		slot := carrier.Define[ilstTestCarrierID]()
		resolver := ilstTestResolver(t, slot)
		document, err := resolver.Parse(t.Context(), slot, "ilst", metadata.AssetScope, metadata.NewBlob(ilstMediaType, payload))
		if err != nil {
			return
		}
		encoded, reports, err := resolver.Marshal(t.Context(), slot, "ilst", document)
		if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
			t.Fatalf("accepted source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
		}
	})
}

func ilstTestResolver(t testing.TB, slot carrier.ID) metadata.Resolver {
	t.Helper()
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{slot: ilstComponent()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func ilstTestData(typeCode, locale uint32, value []byte) []byte {
	payload := binary.BigEndian.AppendUint32(nil, typeCode)
	payload = binary.BigEndian.AppendUint32(payload, locale)
	payload = append(payload, value...)
	return ilstTestAtom(ilstData, payload)
}

func ilstTestItem(typeID ilstType, children ...[]byte) []byte {
	return ilstTestAtom(typeID, bytes.Join(children, nil))
}

func ilstTestLargeItem(typeID ilstType, children ...[]byte) []byte {
	return ilstTestLargeAtom(typeID, bytes.Join(children, nil))
}

func ilstTestLargeAtom(typeID ilstType, payload []byte) []byte {
	result := binary.BigEndian.AppendUint32(nil, 1)
	result = append(result, typeID[:]...)
	result = binary.BigEndian.AppendUint64(result, uint64(16+len(payload)))
	return append(result, payload...)
}

func ilstTestDeclaredLarge(typeID ilstType, size uint64) []byte {
	result := binary.BigEndian.AppendUint32(nil, 1)
	result = append(result, typeID[:]...)
	return binary.BigEndian.AppendUint64(result, size)
}

func ilstTestAtom(typeID ilstType, payload []byte) []byte {
	result := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
	result = append(result, typeID[:]...)
	return append(result, payload...)
}

func mustIlstChild(t testing.TB, payload metadata.Blob, item ilstAtom) ilstAtom {
	t.Helper()
	children, err := ilstScan(payload, item.payloadStart, item.payloadEnd)
	if err != nil || len(children) != 1 {
		t.Fatalf("item children %#v, error %v", children, err)
	}
	return children[0]
}
