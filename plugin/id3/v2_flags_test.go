package id3

import (
	"bytes"
	"errors"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestV2DecodesTagAndFrameUnsynchronisationWithoutTouchingFrameHeaders(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		name    string
		version byte
		flags   byte
		frame   [2]byte
	}{
		{name: "v2.3 tag", version: 3, flags: 0x80},
		{name: "v2.4 tag", version: 4, flags: 0x80},
		{name: "v2.4 frame", version: 4, frame: [2]byte{0, 0x02}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := []byte{0, 'A', 0xff, 0, 'B'}
			if test.version < 4 {
				wire = []byte{0, 'A', 0xff, 'B'}
			}
			frame := v2TestFrame(test.version, map[byte]string{2: "TT2", 3: "TIT2", 4: "TIT2"}[test.version], wire, test.frame)
			if test.version < 4 {
				frame = v2TestUnsynchronise(frame)
			}
			payload := v2TestTagVersion(test.version, test.flags, frame)
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			title, ok := metadata.First(document, tag.Title())
			if !ok || title != "A\u00ffB" {
				t.Fatalf("unsynchronised title = %q/%v", title, ok)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", document)
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
				t.Fatalf("unchanged unsynchronised tag = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
			}
		})
	}
}

func TestV2RejectsStatusAndGroupingFramesWhenEditing(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, flags := range [][2]byte{{0x20, 0}, {0, 0x40}} {
		payload := v2TestTag(v2TestFrame(4, "TIT2", []byte{3, 'T'}, flags))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Entries()) != 0 || len(document.Blocks()) != 2 {
			t.Fatalf("qualified frame became semantic: %#v", document)
		}
		builder := document.Edit()
		metadata.Add(builder, tag.Title(), "Edited", metadata.Origin{})
		edited, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolver.Marshal(t.Context(), slot, "head", edited); !errors.Is(err, errV2Unsupported) {
			t.Fatalf("flagged frame migration error = %v", err)
		}
	}
}

func TestV2FrameDataLengthIndicatorIsSemanticallySafe(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	payload := v2TestTag(v2TestFrame(4, "TIT2", append(v2EncodeSyncSafe(2), 3, 'T'), [2]byte{0, 0x01}))
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	title, ok := metadata.First(document, tag.Title())
	if !ok || title != "T" {
		t.Fatalf("DLI title = %q/%v", title, ok)
	}
}

func TestV2TextRulesAndWXXXQualifier(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		name        string
		payload     []byte
		wantTitle   []string
		wantWebsite string
		semantic    bool
	}{
		{name: "v2.3 rejects UTF-8", payload: v2TestTagVersion(3, 0, v2TestFrame(3, "TIT2", []byte{3, 'T'}, [2]byte{}))},
		{name: "v2.3 rejects multi-value", payload: v2TestTagVersion(3, 0, v2TestFrame(3, "TIT2", []byte{0, 'A', 0, 'B'}, [2]byte{}))},
		{name: "v2.4 keeps multi-value", payload: v2TestTag(v2BuildFrame("TIT2", []byte{3, 'A', 0, 'B'})), wantTitle: []string{"A", "B"}, semantic: true},
		{name: "canonical WXXX", payload: v2TestTag(v2BuildFrame("WXXX", []byte{3, 0, 'h', 't', 't', 'p', ':', '/', '/', 'x'})), wantWebsite: "http://x", semantic: true},
		{name: "qualified WXXX", payload: v2TestTag(v2BuildFrame("WXXX", []byte{3, 'x', 0, 'h'}))},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if got := metadata.Values(document, tag.Title()); !bytes.Equal([]byte(joinV2Values(got)), []byte(joinV2Values(test.wantTitle))) {
				t.Fatalf("title values = %#v, want %#v", got, test.wantTitle)
			}
			website, websiteOK := metadata.First(document, tag.Website())
			if (test.wantWebsite != "") != websiteOK || website != test.wantWebsite {
				t.Fatalf("website = %q/%v, want %q", website, websiteOK, test.wantWebsite)
			}
			if test.semantic != (len(document.Entries()) != 0) {
				t.Fatalf("semantic entries = %#v", document.Entries())
			}
		})
	}
}

func TestV2RetainsReservedFormatFlagsOpaque(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		flags   [2]byte
	}{
		{version: 3, flags: [2]byte{0, 0x01}},
		{version: 4, flags: [2]byte{0, 0x80}},
		{version: 4, flags: [2]byte{0, 0x10}},
	} {
		payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, map[byte]string{3: "TIT2", 4: "TIT2"}[test.version], []byte{0, 'T'}, test.flags))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Entries()) != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
			t.Fatalf("reserved flags became semantic: %#v", document)
		}
	}
}

func TestV2DLIValidatesDecodedDataLength(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	textPayload := append(v2EncodeSyncSafe(2), 3, 'T')
	picturePayload := append(v2EncodeSyncSafe(14), []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0, 0, 0, 1}...)
	for _, test := range []struct {
		name     string
		frameID  string
		payload  []byte
		wantText bool
		wantArt  bool
	}{
		{name: "text", frameID: "TIT2", payload: textPayload, wantText: true},
		{name: "picture", frameID: "APIC", payload: picturePayload, wantArt: true},
		{name: "mismatch", frameID: "TIT2", payload: append(v2EncodeSyncSafe(3), 3, 'T')},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := v2TestTag(v2TestFrame(4, test.frameID, test.payload, [2]byte{0, 0x01}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			_, textOK := metadata.First(document, tag.Title())
			_, pictureOK := metadata.First(document, tag.Picture())
			if textOK != test.wantText || pictureOK != test.wantArt {
				t.Fatalf("DLI document = %#v", document)
			}
			if !test.wantText && !test.wantArt && (len(document.Blocks()) != 2 || document.Blocks()[1].Source()) {
				t.Fatalf("bad DLI was not opaque: %#v", document)
			}
		})
	}
}

func joinV2Values(values []string) string {
	result := ""
	for _, value := range values {
		result += "\x00" + value
	}
	return result
}

func v2TestUnsynchronise(value []byte) []byte {
	result := make([]byte, 0, len(value)+1)
	for _, byteValue := range value {
		result = append(result, byteValue)
		if byteValue == 0xff {
			result = append(result, 0)
		}
	}
	return result
}
