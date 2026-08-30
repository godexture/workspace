package vorbiscomment

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type testCarrierID struct{}
type foreignCarrierID struct{}
type foreignEncodingID struct{}

func TestParseAndUnchangedMarshalPreserveSource(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	payload := testPayload("reference encoder", "TITLE=Song", "ARTIST=First", "ARTIST=Second", "TRACKNUMBER=2/12", "PERFORMER=Band", "DATE=not-a-date")
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "Song" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	if got := metadata.Values(document, tag.Artist()); !reflect.DeepEqual(got, []string{"First", "Second"}) {
		t.Fatalf("artists = %#v", got)
	}
	if number, ok := metadata.First(document, tag.TrackNumber()); !ok || number != 2 {
		t.Fatalf("track = %d/%v", number, ok)
	}
	if total, ok := metadata.First(document, tag.TotalTracks()); !ok || total != 12 {
		t.Fatalf("total = %d/%v", total, ok)
	}
	blocks := document.Blocks()
	if len(blocks) != 4 || !blocks[0].Source() || blocks[1].ID() != vendorBlockID("comment") || blocks[1].Payload().MediaType() != vendorMediaType {
		t.Fatalf("blocks = %#v", blocks)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestRFCFieldSyntaxAndUTF8Values(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	payload := []byte{
		0x20, 0x00, 0x00, 0x00,
		'r', 'e', 'f', 'e', 'r', 'e', 'n', 'c', 'e', ' ', 'l', 'i', 'b', 'F', 'L', 'A', 'C', ' ', '1', '.', '3', '.', '3', ' ', '2', '0', '1', '9', '0', '8', '0', '4',
		0x01, 0x00, 0x00, 0x00,
		0x0e, 0x00, 0x00, 0x00,
		'T', 'I', 'T', 'L', 'E', '=', 0xd7, 0xa9, 0xd7, 0x9c, 0xd7, 0x95, 0xd7, 0x9d,
	}
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := metadata.First(document, tag.Title()); !ok || title != "שלום" {
		t.Fatalf("Hebrew title = %q/%v", title, ok)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("RFC source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	opaquePayload := testPayload("vendor", "~=extension")
	opaque, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, opaquePayload))
	if err != nil {
		t.Fatal(err)
	}
	blocks := opaque.Blocks()
	if len(blocks) != 3 || blocks[2].Payload().MediaType() != fieldMediaType || !bytes.Equal(blocks[2].Payload().AppendTo(nil), []byte("~=extension")) {
		t.Fatalf("RFC opaque field = %#v", blocks)
	}
}

func TestUnsafeSourceIsExactOnly(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	for _, test := range []struct {
		name    string
		payload []byte
	}{
		{name: "invalid vendor UTF-8", payload: testPayload(string([]byte{0xff}))},
		{name: "invalid field UTF-8", payload: testPayload("vendor", string([]byte{'T', 'I', 'T', 'L', 'E', '=', 0xff}))},
		{name: "control field name", payload: testPayload("vendor", "\x1f=value")},
		{name: "non-ASCII field name", payload: testPayload("vendor", "ת=value")},
		{name: "empty field name", payload: testPayload("vendor", "=value")},
		{name: "missing equals", payload: testPayload("vendor", "value")},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, test.payload))
			if err != nil {
				t.Fatal(err)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), test.payload) {
				t.Fatalf("unsafe exact = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
			}
			builder := document.Edit()
			metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
			edited, err := builder.Build()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := resolver.Marshal(t.Context(), slot, "comment", edited); !errors.Is(err, errUnsupported) {
				t.Fatalf("unsafe edit error = %v", err)
			}
		})
	}
}

func TestCanonicalPreservesRawFieldSequenceAndVendor(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	payload := testPayload("source vendor", "TITLE=first", "PERFORMER=Band", "ARTIST=artist")
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("rewrite reports %#v, error %v", reports, err)
	}
	vendor, fields := testFields(t, encoded.AppendTo(nil))
	if vendor != "source vendor" || !reflect.DeepEqual(fields, []string{"TITLE=first", "PERFORMER=Band", "ARTIST=artist", "TITLE=edited"}) {
		t.Fatalf("rewrite = %q %#v", vendor, fields)
	}
}

func TestFreshPreservesEmptyDuplicateAndOrder(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Title(), "", metadata.Origin{})
	metadata.Add(builder, tag.Title(), "second", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "a", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "b", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 0 {
		t.Fatalf("fresh reports %#v, error %v", reports, err)
	}
	vendor, fields := testFields(t, encoded.AppendTo(nil))
	if vendor != defaultVendor || !reflect.DeepEqual(fields, []string{"TITLE=", "TITLE=second", "ARTIST=a", "ARTIST=b"}) {
		t.Fatalf("fresh = %q %#v", vendor, fields)
	}
	parsed, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, encoded)
	if err != nil || !reflect.DeepEqual(metadata.Values(parsed, tag.Title()), []string{"", "second"}) || !reflect.DeepEqual(metadata.Values(parsed, tag.Artist()), []string{"a", "b"}) {
		t.Fatalf("fresh closure = %#v, %v", parsed, err)
	}
}

func TestOrdinalFieldsUseUnsignedDecimalAndFreshSeparation(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	payload := testPayload("vendor", "TRACKNUMBER=0/0", "TRACKTOTAL=3", "DISCNUMBER=2", "TOTALDISCS=4")
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if tracks := metadata.Values(document, tag.TrackNumber()); !reflect.DeepEqual(tracks, []int64{0}) || !reflect.DeepEqual(metadata.Values(document, tag.TotalTracks()), []int64{0, 3}) {
		t.Fatalf("track ordinals = %#v/%#v", tracks, metadata.Values(document, tag.TotalTracks()))
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.TrackNumber(), int64(2), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(12), metadata.Origin{})
	fresh, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := resolver.Marshal(t.Context(), slot, "comment", fresh)
	if err != nil {
		t.Fatal(err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if !reflect.DeepEqual(fields, []string{"TRACKNUMBER=2", "TRACKTOTAL=12"}) {
		t.Fatalf("fresh ordinal fields = %#v", fields)
	}
}

func TestRawSafetyAndForeignOpaquePolicy(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	unsafe := testPayload("vendor", "not-a-field")
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, unsafe))
	if err != nil {
		t.Fatal(err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edit", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "comment", edited); !errors.Is(err, errUnsupported) {
		t.Fatalf("unsafe source edit error = %v", err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), unsafe) {
		t.Fatalf("unsafe source exact = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}

	builder = metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{})
	builder.AddBlock(metadata.NewRawBlock("comment/field/00000099", slot, EncodingIdentity(), metadata.NewBlob(fieldMediaType, []byte("TITLE=injected"))))
	injected, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "comment", injected); !errors.Is(err, errUnsupported) {
		t.Fatalf("semantic raw injection error = %v", err)
	}

	foreign := carrier.Define[foreignCarrierID]()
	builder = metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{})
	builder.AddBlock(metadata.NewRawBlock("foreign", foreign, plugin.IdentityOf[foreignEncodingID](), metadata.NewBlob("application/octet-stream", []byte{1})))
	document, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "comment", document); !errors.Is(err, errUnsupported) {
		t.Fatalf("foreign opaque error = %v", err)
	}
	builder = metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock("foreign", foreign, plugin.IdentityOf[foreignEncodingID](), metadata.NewBlob("application/octet-stream", []byte{1})))
	metadata.Add(builder, tag.Title(), "title", metadata.Origin{})
	document, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 0 {
		t.Fatalf("foreign source error = %v, reports %#v", err, reports)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if !reflect.DeepEqual(fields, []string{"TITLE=title"}) {
		t.Fatalf("foreign source fields = %#v", fields)
	}
}

func TestEditedSourceRetainsSafeRawFields(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	payload := testPayload("vendor", "DATE=not-a-date", "CONTACT=mailto:test@example.invalid", "WAVEFORMATEXTENSIBLE_CHANNEL_MASK=3")
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("safe raw rewrite reports %#v, error %v", reports, err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	want := []string{"DATE=not-a-date", "CONTACT=mailto:test@example.invalid", "WAVEFORMATEXTENSIBLE_CHANNEL_MASK=3", "TITLE=edited"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("safe raw fields = %#v, want %#v", fields, want)
	}
}

func TestParseRejectsStructuralOverflows(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	trailing := append(testPayload("vendor", "TITLE=ok"), 0)
	for _, payload := range [][]byte{
		binary.LittleEndian.AppendUint32(nil, ^uint32(0)),
		[]byte{1, 0, 0, 0, 'v'},
		append(append([]byte{0, 0, 0, 0}, 1, 0, 0, 0), 0),
		append(append([]byte{0, 0, 0, 0}, 0xff, 0xff, 0xff, 0xff), 0),
		append([]byte{0, 0, 0, 0, 1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}, 0),
		testPayload("vendor", "TITLE=ok", "trailing")[:len(testPayload("vendor", "TITLE=ok", "trailing"))-1],
		trailing,
	} {
		if _, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload)); !errors.Is(err, errMalformed) {
			t.Fatalf("structural parse error = %v for %x", err, payload)
		}
	}
}

func testResolver(t testing.TB, slot carrier.ID) metadata.Resolver {
	t.Helper()
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{slot: component()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func testPayload(vendor string, fields ...string) []byte {
	result := vcAppendString(nil, vendor)
	result = binary.LittleEndian.AppendUint32(result, uint32(len(fields)))
	for _, field := range fields {
		result = vcAppendString(result, field)
	}
	return result
}

func testFields(t testing.TB, value []byte) (string, []string) {
	t.Helper()
	reader := vcReader{blob: metadata.NewBlob(mediaType, value)}
	start, end, ok := reader.stringRange()
	if !ok {
		t.Fatal("no vendor")
	}
	vendor := string(value[start:end])
	count, ok := reader.u32()
	if !ok {
		t.Fatal("no count")
	}
	fields := make([]string, 0, count)
	for range count {
		start, end, ok := reader.stringRange()
		if !ok {
			t.Fatal("truncated field")
		}
		fields = append(fields, string(value[start:end]))
	}
	if reader.offset != len(value) {
		t.Fatal("trailing wire data")
	}
	return vendor, fields
}

func TestUnsupportedSemanticReportsOrigin(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Website(), "https://example.invalid", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	_, reports, err := resolver.Marshal(t.Context(), slot, "comment", document)
	if err != nil || len(reports) != 1 || reports[0].Loss.Kind != loss.Dropped {
		t.Fatalf("unsupported reports %#v, error %v", reports, err)
	}
}

func TestMarshalLossReportsCarryEveryField(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	input := metadata.BlockID("input")
	origin := metadata.Origin{Carrier: slot, Encoding: EncodingIdentity(), Block: input, Native: "source"}
	builder := metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock(input, slot, EncodingIdentity(), metadata.NewBlob(mediaType, nil)))
	metadata.Add(builder, tag.Title(), string([]byte{0xff}), origin)
	metadata.Add(builder, tag.Date(), tag.PartialDate{}, origin)
	metadata.Add(builder, tag.TrackNumber(), int64(-1), origin)
	metadata.Add(builder, tag.TotalTracks(), int64(-1), origin)
	metadata.Add(builder, tag.DiscNumber(), int64(-1), origin)
	metadata.Add(builder, tag.TotalDiscs(), int64(-1), origin)
	metadata.Add(builder, tag.Picture(), tag.Artwork{}, origin)
	metadata.Add(builder, tag.Website(), "https://example.invalid", origin)
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	_, reports, err := resolver.Marshal(t.Context(), slot, "output", document)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		key    key.ID
		native string
		detail string
	}{
		{key: tag.Title().ID(), native: "TITLE", detail: "vorbiscomment.text-unrepresentable"},
		{key: tag.Date().ID(), native: "DATE", detail: "vorbiscomment.date-unrepresentable"},
		{key: tag.TrackNumber().ID(), native: "TRACKNUMBER", detail: "vorbiscomment.number-unrepresentable"},
		{key: tag.TotalTracks().ID(), native: "TRACKTOTAL", detail: "vorbiscomment.number-unrepresentable"},
		{key: tag.DiscNumber().ID(), native: "DISCNUMBER", detail: "vorbiscomment.number-unrepresentable"},
		{key: tag.TotalDiscs().ID(), native: "DISCTOTAL", detail: "vorbiscomment.number-unrepresentable"},
		{key: tag.Picture().ID(), native: "METADATA_BLOCK_PICTURE", detail: "vorbiscomment.picture-unrepresentable"},
		{key: tag.Website().ID(), native: "", detail: "vorbiscomment.unrepresentable"},
	}
	if len(reports) != len(want) {
		t.Fatalf("loss reports = %#v", reports)
	}
	for index, expected := range want {
		report := reports[index]
		if !report.Valid() || report.Carrier != slot || report.Encoding != EncodingIdentity().String() || report.Block != "output" || report.Loss.Key != expected.key || report.Loss.Kind != loss.Dropped || report.Loss.Native != expected.native || report.Loss.Detail != expected.detail || report.Loss.Source.Carrier != slot || report.Loss.Source.Encoding != EncodingIdentity().String() || report.Loss.Source.Block != string(input) || report.Loss.Source.Native != "source" {
			t.Fatalf("loss report %d = %#v", index, report)
		}
	}
}
