package id3

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/testkit"
)

type v1TestCarrierID struct{}
type v1ForeignCarrierID struct{}
type v1ForeignEncodingID struct{}

func TestV1ParseAndUnchangedMarshalPreserveTheTagBytes(t *testing.T) {
	payload := v1TestPayload(t)
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	document, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload))
	if err != nil {
		t.Fatal(err)
	}
	blocks := document.Blocks()
	if len(blocks) != 1 || !blocks[0].Source() || !bytes.Equal(blocks[0].Payload().AppendTo(nil), payload) {
		t.Fatalf("ID3v1 source blocks = %#v", blocks)
	}
	for _, test := range []struct {
		name   string
		key    key.ID
		want   any
		native string
	}{
		{name: "title", key: tag.Title().ID(), want: "Title", native: "title"},
		{name: "artist", key: tag.Artist().ID(), want: "Artist", native: "artist"},
		{name: "album", key: tag.Album().ID(), want: "Album", native: "album"},
		{name: "comment", key: tag.Comment().ID(), want: "Comment", native: "comment"},
		{name: "track", key: tag.TrackNumber().ID(), want: int64(7), native: "track"},
		{name: "genre", key: tag.Genre().ID(), want: "Rock", native: "genre"},
	} {
		entry, ok := v1TestEntry(document, test.key, test.want)
		if !ok {
			t.Errorf("%s = %#v, want %#v", test.name, document.Entries(), test.want)
			continue
		}
		wantOrigin := metadata.Origin{Carrier: slot, Encoding: V1EncodingIdentity(), Block: "tail", Native: test.native}
		if entry.Origin() != wantOrigin {
			t.Errorf("%s origin = %#v, want %#v", test.name, entry.Origin(), wantOrigin)
		}
	}
	date, ok := v1TestEntry(document, tag.Date().ID(), mustV1Date(t, "2024"))
	if !ok {
		t.Fatalf("ID3v1 date = %#v", document.Entries())
	}
	wantDateOrigin := metadata.Origin{Carrier: slot, Encoding: V1EncodingIdentity(), Block: "tail", Native: "year"}
	if date.Origin() != wantDateOrigin {
		t.Fatalf("ID3v1 date origin = %#v, want %#v", date.Origin(), wantDateOrigin)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("unchanged ID3v1 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1MarshalSelectsFirstRepresentableValuesAndReportsActualLosses(t *testing.T) {
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	date, err := tag.ParseDate("2024-07-04")
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Title(), "", metadata.Origin{})
	metadata.Add(builder, tag.Title(), "é\x00"+strings.Repeat("x", 30), metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "First", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "Second", metadata.Origin{})
	metadata.Add(builder, tag.Date(), date, metadata.Origin{})
	metadata.Add(builder, tag.Genre(), "", metadata.Origin{})
	metadata.Add(builder, tag.Genre(), "unknown", metadata.Origin{})
	metadata.Add(builder, tag.Genre(), "rock", metadata.Origin{})
	metadata.Add(builder, tag.Comment(), "Comment", metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(0), metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(9), metadata.Origin{})
	metadata.Add(builder, tag.Composer(), "Dropped", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil {
		t.Fatal(err)
	}
	value := encoded.AppendTo(nil)
	wantTitle := append([]byte{0xe9, '?'}, bytes.Repeat([]byte{'x'}, 28)...)
	if len(value) != v1Size || string(value[:3]) != v1Tag || !bytes.Equal(value[3:33], wantTitle) || string(value[33:38]) != "First" || string(value[93:97]) != "2024" || string(value[97:104]) != "Comment" || value[125] != 0 || value[126] != 9 || value[127] != 17 {
		t.Fatalf("ID3v1 bytes = %x", value)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{
		{Key: tag.Title().ID(), Kind: loss.Dropped, Native: "title", Detail: "id3v1.text-unrepresentable"},
		{Key: tag.Title().ID(), Kind: loss.Substituted, Native: "title", Detail: "id3v1.text-substituted"},
		{Key: tag.Title().ID(), Kind: loss.Truncated, Native: "title", Detail: "id3v1.text-truncated"},
		{Key: tag.Artist().ID(), Kind: loss.Folded, Native: "artist", Detail: "id3v1.single-value"},
		{Key: tag.Date().ID(), Kind: loss.Truncated, Native: "year", Detail: "id3v1.date-year"},
		{Key: tag.Genre().ID(), Kind: loss.Dropped, Native: "genre", Detail: "id3v1.genre-unrepresentable"},
		{Key: tag.Genre().ID(), Kind: loss.Dropped, Native: "genre", Detail: "id3v1.genre-unrepresentable"},
		{Key: tag.Genre().ID(), Kind: loss.Substituted, Native: "genre", Detail: "id3v1.genre-substituted"},
		{Key: tag.TrackNumber().ID(), Kind: loss.Dropped, Native: "track", Detail: "id3v1.track-unrepresentable"},
		{Key: tag.Composer().ID(), Kind: loss.Dropped, Detail: "id3v1.unrepresentable"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ID3v1 losses = %#v, want %#v", got, want)
	}
	for _, report := range reports {
		if report.Carrier != slot || report.Encoding != V1EncodingIdentity().String() || report.Block != "tail" {
			t.Fatalf("ID3v1 report target = %#v", report)
		}
	}
}

func TestV1RejectsMalformedPayloadAndOpaqueBlocks(t *testing.T) {
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	for _, payload := range [][]byte{
		make([]byte, v1Size-1),
		make([]byte, v1Size+1),
		append([]byte("BAD"), make([]byte, v1Size-3)...),
	} {
		if _, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload)); !errors.Is(err, errV1Malformed) {
			t.Fatalf("malformed ID3v1 payload error = %v", err)
		}
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewRawBlock("foreign", slot, V1EncodingIdentity(), metadata.NewBlob("application/octet-stream", []byte{1})))
	metadata.Add(builder, tag.Title(), "Title", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "tail", document); !errors.Is(err, errV1Unsupported) {
		t.Fatalf("opaque ID3v1 block error = %v", err)
	}
}

func TestV1ParsePreservesWhitespaceAndInternalNUL(t *testing.T) {
	payload := v1TestPayload(t)
	copy(payload[3:33], append([]byte("  A\x00B  "), make([]byte, 23)...))
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	document, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload))
	if err != nil {
		t.Fatal(err)
	}
	title, ok := metadata.First(document, tag.Title())
	if !ok || title != "  A\x00B  " {
		t.Fatalf("ID3v1 title = %q/%v", title, ok)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("whitespace-preserving ID3v1 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1CanonicalizationReportsUnparsedSourceFields(t *testing.T) {
	payload := v1TestPayload(t)
	copy(payload[93:97], "bad!")
	payload[127] = 200
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	parsed, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload))
	if err != nil {
		t.Fatal(err)
	}
	unchanged, reports, err := resolver.Marshal(t.Context(), slot, "tail", parsed)
	if err != nil || len(reports) != 0 || !bytes.Equal(unchanged.AppendTo(nil), payload) {
		t.Fatalf("unchanged invalid source = %x, reports %#v, error %v", unchanged.AppendTo(nil), reports, err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range parsed.Blocks() {
		builder.AddBlock(block)
	}
	origin := func(native string) metadata.Origin {
		return metadata.Origin{Carrier: slot, Encoding: V1EncodingIdentity(), Block: "tail", Native: native}
	}
	metadata.Add(builder, tag.Title(), "Edited", origin("title"))
	metadata.Add(builder, tag.Artist(), "Artist", origin("artist"))
	metadata.Add(builder, tag.Album(), "Album", origin("album"))
	metadata.Add(builder, tag.Comment(), "Comment", origin("comment"))
	metadata.Add(builder, tag.TrackNumber(), int64(7), origin("track"))
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", edited)
	if err != nil || string(encoded.AppendTo(nil)[3:9]) != "Edited" {
		t.Fatalf("canonicalized invalid source = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	source := loss.Origin{Carrier: slot, Encoding: V1EncodingIdentity().String(), Block: "tail"}
	want := []loss.Report{
		{Carrier: slot, Encoding: V1EncodingIdentity().String(), Block: "tail", Loss: loss.Loss{Key: tag.Date().ID(), Kind: loss.Dropped, Native: "year", Detail: "id3v1.date-unparsed", Source: loss.Origin{Carrier: source.Carrier, Encoding: source.Encoding, Block: source.Block, Native: "year"}}},
		{Carrier: slot, Encoding: V1EncodingIdentity().String(), Block: "tail", Loss: loss.Loss{Key: tag.Genre().ID(), Kind: loss.Dropped, Native: "genre", Detail: "id3v1.genre-unparsed", Source: loss.Origin{Carrier: source.Carrier, Encoding: source.Encoding, Block: source.Block, Native: "genre"}}},
	}
	if !reflect.DeepEqual(reports, want) {
		t.Fatalf("canonicalization reports = %#v, want %#v", reports, want)
	}
}

func TestV1ParsesV10Comment(t *testing.T) {
	payload := v1TestPayload(t)
	copy(payload[97:127], []byte("v1.0 comment"))
	payload[125] = 'x'
	payload[126] = 'y'
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	document, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload))
	if err != nil {
		t.Fatal(err)
	}
	comment, ok := metadata.First(document, tag.Comment())
	if !ok || !strings.HasPrefix(comment, "v1.0 comment") {
		t.Fatalf("ID3v1.0 comment = %q/%v", comment, ok)
	}
	if _, ok := metadata.First(document, tag.TrackNumber()); ok {
		t.Fatal("ID3v1.0 comment unexpectedly has a track number")
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("ID3v1.0 roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1FreshEmptyDocumentIsAbsent(t *testing.T) {
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	document, err := metadata.NewBuilder(metadata.StreamScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || encoded.Len() != 0 || encoded.MediaType() != "application/x-id3v1" {
		t.Fatalf("fresh empty ID3v1 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1CanonicalDocumentWithoutRepresentableEntriesIsAbsent(t *testing.T) {
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Composer(), "unsupported", metadata.Origin{})
	metadata.Add(builder, tag.Title(), "", metadata.Origin{})
	metadata.Add(builder, tag.Genre(), "not-a-genre", metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(0), metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || encoded.Len() != 0 || encoded.MediaType() != "application/x-id3v1" {
		t.Fatalf("unrepresentable ID3v1 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{
		{Key: tag.Composer().ID(), Kind: loss.Dropped, Detail: "id3v1.unrepresentable"},
		{Key: tag.Title().ID(), Kind: loss.Dropped, Native: "title", Detail: "id3v1.text-unrepresentable"},
		{Key: tag.Genre().ID(), Kind: loss.Dropped, Native: "genre", Detail: "id3v1.genre-unrepresentable"},
		{Key: tag.TrackNumber().ID(), Kind: loss.Dropped, Native: "track", Detail: "id3v1.track-unrepresentable"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unrepresentable ID3v1 losses = %#v, want %#v", got, want)
	}
}

func TestV1ExistingEmptyTagRemainsByteExact(t *testing.T) {
	payload := v1EmptyPayload()
	slot := carrier.Define[v1TestCarrierID]()
	resolver := v1TestResolver(t, slot)
	document, err := resolver.Parse(t.Context(), slot, "tail", metadata.StreamScope, metadata.NewBlob("application/x-id3v1", payload))
	if err != nil || document.Len() != 0 {
		t.Fatalf("empty ID3v1 Parse = %#v, %v", document, err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("empty ID3v1 roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1IgnoresForeignSourceBlocksWithCollidingBlockID(t *testing.T) {
	slot := carrier.Define[v1TestCarrierID]()
	foreignSlot := carrier.Define[v1ForeignCarrierID]()
	foreignEncoding := plugin.IdentityOf[v1ForeignEncodingID]()
	resolver := v1TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewSourceBlock("tail", foreignSlot, foreignEncoding, metadata.NewBlob("application/octet-stream", []byte{1})))
	metadata.Add(builder, tag.Title(), "Title", metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "tail", document)
	if err != nil || len(reports) != 0 || string(encoded.AppendTo(nil)[:3]) != v1Tag {
		t.Fatalf("foreign source ID3v1 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV1GenreTableAndLegacyAlias(t *testing.T) {
	if len(v1Genres) != 148 {
		t.Fatalf("ID3v1 genre table length = %d, want 148", len(v1Genres))
	}
	for index, want := range append([]string{"Blues", "Psychedelic"}, v1Genres[126:]...) {
		actual := index
		if index >= 2 {
			actual += 124
		} else if index == 1 {
			actual = 67
		}
		genre, ok := decodeV1Genre(byte(actual))
		if !ok || genre != want {
			t.Fatalf("genre %d = %q/%v, want %q", actual, genre, ok, want)
		}
		encoded, canonical, ok := encodeV1Genre(want)
		if !ok || int(encoded) != actual || canonical != want {
			t.Fatalf("encode genre %q = %d/%q/%v", want, encoded, canonical, ok)
		}
	}
	encoded, canonical, ok := encodeV1Genre("Psychadelic")
	if !ok || encoded != 67 || canonical != "Psychedelic" {
		t.Fatalf("legacy psychedelic alias = %d/%q/%v", encoded, canonical, ok)
	}
}

func TestPluginAndSetComposeWithoutAnMP3Binding(t *testing.T) {
	testkit.Plugin(t, Plugin())
	actual, err := host.New(host.Plugins(Set()))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := host.New(host.Plugins(plugin.NewSet(Plugin())))
	if err != nil {
		t.Fatal(err)
	}
	if actual.Catalog().Fingerprint() != expected.Catalog().Fingerprint() {
		t.Fatal("ID3 Set differs from its owned Plugin composition")
	}
}

func v1TestResolver(t testing.TB, slot carrier.ID) metadata.Resolver {
	t.Helper()
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{slot: v1Component()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func v1TestPayload(t testing.TB) []byte {
	t.Helper()
	value := make([]byte, v1Size)
	copy(value, v1Tag)
	copy(value[3:33], []byte("Title"))
	copy(value[33:63], []byte("Artist"))
	copy(value[63:93], []byte("Album"))
	copy(value[93:97], []byte("2024"))
	copy(value[97:125], []byte("Comment"))
	value[125] = 0
	value[126] = 7
	value[127] = 17
	return value
}

func v1EmptyPayload() []byte {
	value := make([]byte, v1Size)
	copy(value, v1Tag)
	value[127] = 255
	return value
}

func v1TestEntry(document metadata.Document, identity key.ID, want any) (metadata.Entry, bool) {
	for _, entry := range document.Entries() {
		if entry.Key() == identity && reflect.DeepEqual(entry.Value(), want) {
			return entry, true
		}
	}
	return metadata.Entry{}, false
}

func mustV1Date(t testing.TB, value string) tag.PartialDate {
	t.Helper()
	date, err := tag.ParseDate(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
