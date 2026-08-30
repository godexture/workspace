package id3

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
)

func TestV2KnownAndQualifierFrameMatrix(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		textIDs []string
		track   string
		disc    string
		opaque  []string
	}{
		{version: 2, textIDs: []string{"TT2", "TP1", "TAL", "TCM", "TCO", "TCR", "TEN"}, track: "TRK", disc: "TPA", opaque: []string{"TP2", "TP3", "TP4", "WAF", "WAR", "WAS"}},
		{version: 3, textIDs: []string{"TIT2", "TPE1", "TALB", "TCOM", "TCON", "TCOP", "TENC"}, track: "TRCK", disc: "TPOS", opaque: []string{"TPE2", "TPE3", "TPE4", "WOAF", "WOAR", "WOAS"}},
		{version: 4, textIDs: []string{"TIT2", "TPE1", "TALB", "TCOM", "TCON", "TCOP", "TENC"}, track: "TRCK", disc: "TPOS", opaque: []string{"TPE2", "TPE3", "TPE4", "WOAF", "WOAR", "WOAS"}},
	} {
		for _, frameID := range test.textIDs {
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, frameID, []byte{0, 'V'}, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil || len(document.Entries()) != 1 {
				t.Fatalf("v2.%d %s = %#v, %v", test.version, frameID, document, err)
			}
		}
		for _, frameID := range []string{test.track, test.disc} {
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, frameID, []byte{0, '1', '/', '2'}, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil || len(document.Entries()) != 2 {
				t.Fatalf("v2.%d %s = %#v, %v", test.version, frameID, document, err)
			}
		}
		for _, frameID := range test.opaque {
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, frameID, []byte{0, 'V'}, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil || len(document.Entries()) != 0 || len(document.Blocks()) != 2 {
				t.Fatalf("v2.%d %s was not opaque: %#v, %v", test.version, frameID, document, err)
			}
		}
	}
}
