package id3

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
)

type v2ForeignCarrierID struct{}

func TestV2FreshCanonicalizesTextDateQualifiersAndOrdinalPairs(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	date, err := tag.ParseDate("2024-06-17")
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Title(), "First", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "A", metadata.Origin{})
	metadata.Add(builder, tag.Artist(), "B", metadata.Origin{})
	metadata.Add(builder, tag.Date(), date, metadata.Origin{})
	metadata.Add(builder, tag.Comment(), "Comment", metadata.Origin{})
	metadata.Add(builder, tag.Lyrics(), "Lyrics", metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(1), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(10), metadata.Origin{})
	metadata.Add(builder, tag.DiscNumber(), int64(2), metadata.Origin{})
	metadata.Add(builder, tag.TotalDiscs(), int64(3), metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 {
		t.Fatalf("fresh ID3v2 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	for _, want := range [][]byte{
		[]byte("TIT2"), []byte("TPE1"), []byte("TPE1"), []byte("TDRC"),
		[]byte{3, 'X', 'X', 'X', 0, 'C', 'o', 'm', 'm', 'e', 'n', 't'},
		[]byte{3, 'X', 'X', 'X', 0, 'L', 'y', 'r', 'i', 'c', 's'},
		[]byte{3, '1', '/', '1', '0'}, []byte{3, '2', '/', '3'},
	} {
		if !bytes.Contains(value, want) {
			t.Fatalf("canonical ID3v2 has no %q: %x", want, value)
		}
	}
}

func TestV2OrdinalTotalsWithoutNumbersAreDropped(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.TotalTracks(), int64(10), metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || encoded.Len() != 0 {
		t.Fatalf("total-only ID3v2 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{{Key: tag.TotalTracks().ID(), Kind: loss.Dropped, Native: "TRCK", Detail: "id3v2.total-without-number"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("total-only reports = %#v, want %#v", got, want)
	}
}

func TestV2ReinsertsSafeOpaqueFrameAfterGroupedMultiValueText(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	unknown := v2BuildFrame("XAAA", []byte{1, 2, 3})
	payload := v2TestTag(v2BuildFrame("TIT2", []byte{3, 'A', 0, 'B'}), unknown)
	parsed, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range parsed.Blocks() {
		builder.AddBlock(block)
	}
	origin := metadata.Origin{Carrier: slot, Encoding: V2EncodingIdentity(), Block: "head", Native: "TIT2"}
	metadata.Add(builder, tag.Title(), "A!", origin)
	metadata.Add(builder, tag.Title(), "B", origin)
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("edited ID3v2 = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	first := bytes.Index(value, []byte("TIT2"))
	unknownAt := bytes.Index(value, []byte("XAAA"))
	if first < 0 || bytes.Count(value, []byte("TIT2")) != 1 || unknownAt < first {
		t.Fatalf("opaque frame was not retained after grouped multi-values: %x", value)
	}
}

func TestV2RejectsUnsafeOrForeignOpaqueFramesDuringCanonicalization(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	unsafe := v2TestTagVersion(3, 0, v2TestFrame(3, "XAAA", []byte{1}, [2]byte{}))
	parsed, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, unsafe))
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	for _, block := range parsed.Blocks() {
		builder.AddBlock(block)
	}
	metadata.Add(builder, tag.Title(), "Edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "head", edited); !errors.Is(err, errV2Unsupported) {
		t.Fatalf("unsafe opaque frame error = %v", err)
	}
	foreign := carrier.Define[v2ForeignCarrierID]()
	builder = metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewRawBlock("foreign", foreign, V2EncodingIdentity(), metadata.NewBlob(v2RawMediaType, v2BuildFrame("XAAA", []byte{1}))))
	metadata.Add(builder, tag.Title(), "Title", metadata.Origin{})
	edited, err = builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Marshal(t.Context(), slot, "head", edited); !errors.Is(err, errV2Unsupported) {
		t.Fatalf("foreign opaque frame error = %v", err)
	}
}

func TestV2IgnoresForeignSourceBlocksDuringFreshMarshal(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	foreign := carrier.Define[v2ForeignCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewSourceBlock("foreign", foreign, V2EncodingIdentity(), metadata.NewBlob(v2MediaType, v2TestTag())))
	metadata.Add(builder, tag.Title(), "Title", metadata.Origin{Carrier: foreign, Encoding: V2EncodingIdentity(), Block: "foreign", Native: "TIT2"})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 || !bytes.Contains(encoded.AppendTo(nil), []byte("TIT2")) {
		t.Fatalf("foreign source fresh marshal = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
}

func TestV2CanonicalGroupsTextDateAndOrdinalValues(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	firstDate, err := tag.ParseDate("2024")
	if err != nil {
		t.Fatal(err)
	}
	secondDate, err := tag.ParseDate("2025-06")
	if err != nil {
		t.Fatal(err)
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Title(), "A", metadata.Origin{})
	metadata.Add(builder, tag.Title(), "B", metadata.Origin{})
	metadata.Add(builder, tag.Date(), firstDate, metadata.Origin{})
	metadata.Add(builder, tag.Date(), secondDate, metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(0), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(0), metadata.Origin{})
	metadata.Add(builder, tag.TrackNumber(), int64(2), metadata.Origin{})
	metadata.Add(builder, tag.TotalTracks(), int64(3), metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 {
		t.Fatalf("grouped canonical = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	for _, test := range []struct {
		frame string
		body  []byte
	}{
		{frame: "TIT2", body: []byte{3, 'A', 0, 'B'}},
		{frame: "TDRC", body: []byte{3, '2', '0', '2', '4', 0, '2', '0', '2', '5', '-', '0', '6'}},
		{frame: "TRCK", body: []byte{3, '0', '/', '0', 0, '2', '/', '3'}},
	} {
		if bytes.Count(value, []byte(test.frame)) != 1 || !bytes.Contains(value, test.body) {
			t.Fatalf("canonical %s = %x", test.frame, value)
		}
	}
}

func TestV2EditGroupsRepeatedSourceTextFrames(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	source := v2TestTag(v2BuildFrame("TPE1", []byte{3, 'A'}), v2BuildFrame("TPE1", []byte{3, 'B'}))
	parsed, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, source))
	if err != nil {
		t.Fatal(err)
	}
	builder := parsed.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("group repeated source = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	if bytes.Count(value, []byte("TPE1")) != 1 || !bytes.Contains(value, []byte{3, 'A', 0, 'B'}) {
		t.Fatalf("repeated TPE1 was not grouped: %x", value)
	}
}

func TestV2CanonicalFoldsCanonicalQualifiersAndDuplicatePictures(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Comment(), "first", metadata.Origin{})
	metadata.Add(builder, tag.Comment(), "second", metadata.Origin{})
	metadata.Add(builder, tag.Website(), "https://first", metadata.Origin{})
	metadata.Add(builder, tag.Website(), "https://second", metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{1}), MediaType: "image/png", Description: "cover"}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{2}), MediaType: "image/png", Description: "cover", Width: 9}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{3}), MediaType: "image/png", Type: tag.ArtworkFileIcon, Description: "first icon"}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{4}), MediaType: "image/png", Type: tag.ArtworkFileIcon, Description: "second icon"}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil {
		t.Fatal(err)
	}
	value := encoded.AppendTo(nil)
	if bytes.Count(value, []byte("COMM")) != 1 || bytes.Count(value, []byte("WXXX")) != 1 || bytes.Count(value, []byte("APIC")) != 2 {
		t.Fatalf("unique canonical frames = %x", value)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{
		{Key: tag.Comment().ID(), Kind: loss.Folded, Native: "COMM", Detail: "id3v2.qualifier-folded"},
		{Key: tag.Website().ID(), Kind: loss.Folded, Native: "WXXX", Detail: "id3v2.qualifier-folded"},
		{Key: tag.Picture().ID(), Kind: loss.Folded, Native: "APIC", Detail: "id3v2.picture-folded"},
		{Key: tag.Picture().ID(), Kind: loss.Folded, Native: "APIC", Detail: "id3v2.picture-folded"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folded reports = %#v, want %#v", got, want)
	}
}

func TestV2CanonicalPictureGroupsAfterDescriptionSubstitution(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{1}), MediaType: "image/png", Description: "cover\x00"}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{2}), MediaType: "image/png", Description: "cover�"}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || bytes.Count(encoded.AppendTo(nil), []byte("APIC")) != 1 {
		t.Fatalf("canonical picture = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{
		{Key: tag.Picture().ID(), Kind: loss.Substituted, Native: "APIC", Detail: "id3v2.text-substituted"},
		{Key: tag.Picture().ID(), Kind: loss.Folded, Native: "APIC", Detail: "id3v2.picture-folded"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picture reports = %#v, want %#v", got, want)
	}
}
