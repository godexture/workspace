package id3

import (
	"bytes"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestV2ParsesTitleAcrossSupportedVersions(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		name    string
		version byte
		frameID string
	}{
		{name: "v2.2", version: 2, frameID: "TT2"},
		{name: "v2.3", version: 3, frameID: "TIT2"},
		{name: "v2.4", version: 4, frameID: "TIT2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			textEncoding := byte(0)
			if test.version == 4 {
				textEncoding = 3
			}
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, test.frameID, []byte{textEncoding, 'T', 'i', 't', 'l', 'e'}, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			title, ok := metadata.First(document, tag.Title())
			if !ok || title != "Title" {
				t.Fatalf("ID3v2.%d title = %q/%v", test.version, title, ok)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
				t.Fatalf("ID3v2.%d exact = %x, reports %#v, error %v", test.version, encoded.AppendTo(nil), reports, err)
			}
		})
	}
}

func TestV2PreservesFooterUntilSemanticEditCanonicalizesIt(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	frame := v2BuildFrame("TIT2", []byte{3, 'T'})
	header := append([]byte{'I', 'D', '3', 4, 0, 0x10}, v2EncodeSyncSafe(len(frame))...)
	payload := append(header, frame...)
	payload = append(payload, '3', 'D', 'I', 4, 0, 0x10)
	payload = append(payload, v2EncodeSyncSafe(len(frame))...)
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
	if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
		t.Fatalf("footer source roundtrip = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Artist(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "head", edited)
	if err != nil || len(reports) != 0 {
		t.Fatalf("footer source edit = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	if len(value) < v2HeaderSize || value[5]&0x10 != 0 || bytes.Contains(value, []byte("3DI")) {
		t.Fatalf("canonical output retained footer = %x", value)
	}
}

func TestV2TextFrameEncodingMatrix(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		name    string
		version byte
		frameID string
		body    []byte
	}{
		{name: "v2.2 utf16-le", version: 2, frameID: "TT2", body: []byte{1, 0xff, 0xfe, 'A', 0, 0x22, 0x6f}},
		{name: "v2.2 utf16-be", version: 2, frameID: "TT2", body: []byte{1, 0xfe, 0xff, 0, 'A', 0x6f, 0x22}},
		{name: "v2.3 utf16-le", version: 3, frameID: "TIT2", body: []byte{1, 0xff, 0xfe, 'A', 0, 0x22, 0x6f}},
		{name: "v2.3 utf16-be", version: 3, frameID: "TIT2", body: []byte{1, 0xfe, 0xff, 0, 'A', 0x6f, 0x22}},
		{name: "v2.4 utf16-le", version: 4, frameID: "TIT2", body: []byte{1, 0xff, 0xfe, 'A', 0, 0x22, 0x6f}},
		{name: "v2.4 utf16-be-bom", version: 4, frameID: "TIT2", body: []byte{1, 0xfe, 0xff, 0, 'A', 0x6f, 0x22}},
		{name: "v2.4 utf16-be", version: 4, frameID: "TIT2", body: []byte{2, 0, 'A', 0x6f, 0x22}},
		{name: "v2.4 utf8", version: 4, frameID: "TIT2", body: append([]byte{3}, []byte("A漢")...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, test.frameID, test.body, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			title, ok := metadata.First(document, tag.Title())
			if !ok || title != "A漢" {
				t.Fatalf("%s title = %q/%v", test.name, title, ok)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
				t.Fatalf("%s exact = %x, reports %#v, error %v", test.name, encoded.AppendTo(nil), reports, err)
			}
		})
	}
}

func TestV2MalformedUTF16TextFramesRemainOpaque(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		frameID string
		body    []byte
	}{
		{version: 2, frameID: "TT2", body: []byte{1, 0xff, 0xfe, 'A'}},
		{version: 3, frameID: "TIT2", body: []byte{1, 0xfe, 0xff, 0xd8, 0}},
		{version: 4, frameID: "TIT2", body: []byte{2, 0xdc, 0}},
		{version: 4, frameID: "TIT2", body: []byte{1, 0xff, 0xfe, 0, 0xdc}},
	} {
		payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, test.frameID, test.body, [2]byte{}))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Entries()) != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
			t.Fatalf("malformed UTF-16 became semantic: %#v", document)
		}
	}
}

func TestV2ParsesOnlyCanonicalQualifiedCommentAndLyrics(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	canonical := v2TestTagVersion(4, 0,
		v2BuildFrame("COMM", []byte{3, 'X', 'X', 'X', 0, 'C'}),
		v2BuildFrame("USLT", []byte{3, 'X', 'X', 'X', 0, 'L'}),
	)
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, canonical))
	if err != nil {
		t.Fatal(err)
	}
	comment, commentOK := metadata.First(document, tag.Comment())
	lyrics, lyricsOK := metadata.First(document, tag.Lyrics())
	if !commentOK || comment != "C" || !lyricsOK || lyrics != "L" {
		t.Fatalf("canonical qualifiers = %#v", document.Entries())
	}
	nonCanonical := v2TestTagVersion(4, 0, v2BuildFrame("COMM", []byte{3, 'e', 'n', 'g', 0, 'C'}))
	document, err = resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, nonCanonical))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := metadata.First(document, tag.Comment()); ok || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
		t.Fatalf("qualified COMM was not retained opaque: %#v", document)
	}
}

func TestV2CombinesLegacyDateTuplesByOrdinal(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		name    string
		version byte
		year    string
		day     string
		time    string
	}{
		{name: "v2.2", version: 2, year: "TYE", day: "TDA", time: "TIM"},
		{name: "v2.3", version: 3, year: "TYER", day: "TDAT", time: "TIME"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := v2TestTagVersion(test.version, 0,
				v2TestFrame(test.version, test.year, []byte{0, '2', '0', '2', '4'}, [2]byte{}),
				v2TestFrame(test.version, test.day, []byte{0, '1', '7', '0', '6'}, [2]byte{}),
				v2TestFrame(test.version, test.time, []byte{0, '1', '2', '3', '4'}, [2]byte{}),
			)
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			dates := metadata.Values(document, tag.Date())
			if len(dates) != 1 || dates[0].ToISOString() != "2024-06-17T12:34" || document.Entries()[0].Origin().Native != test.year {
				t.Fatalf("legacy date = %#v, entries %#v", dates, document.Entries())
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
				t.Fatalf("legacy date exact = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
			}
		})
	}
}

func TestV2RetainsUnpairedLegacyDatePartsOpaque(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	payload := v2TestTagVersion(3, 0, v2TestFrame(3, "TDAT", []byte{0, '1', '7', '0', '6'}, [2]byte{}))
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Entries()) != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
		t.Fatalf("unpaired date became semantic: %#v", document)
	}
}
