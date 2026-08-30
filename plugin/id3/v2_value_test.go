package id3

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestV2TDRCRequiresV24PlainTimestamp(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		value   string
		ok      bool
	}{
		{version: 4, value: "2024-06-17T12:34", ok: true},
		{version: 4, value: "2024-06-17T12:34:56", ok: true},
		{version: 4, value: "2024-06-17T12:34Z"},
		{version: 4, value: "2024-06-17T12:34:56.1"},
		{version: 4, value: "17 Jun 2024"},
		{version: 3, value: "2024"},
	} {
		payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, "TDRC", append([]byte{0}, []byte(test.value)...), [2]byte{}))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		_, found := metadata.First(document, tag.Date())
		if found != test.ok {
			t.Fatalf("v2.%d TDRC %q = %#v", test.version, test.value, document)
		}
	}
}

func TestV2OrdinalGrammarAllowsZeroButRejectsSigns(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		value string
		ok    bool
	}{
		{value: "0/0", ok: true},
		{value: "-1", ok: false},
		{value: "+1", ok: false},
	} {
		payload := v2TestTag(v2BuildFrame("TRCK", append([]byte{3}, []byte(test.value)...)))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		numbers := metadata.Values(document, tag.TrackNumber())
		totals := metadata.Values(document, tag.TotalTracks())
		if test.ok != (len(numbers) == 1 && numbers[0] == 0 && len(totals) == 1 && totals[0] == 0) {
			t.Fatalf("TRCK %q = %#v", test.value, document)
		}
	}
}
