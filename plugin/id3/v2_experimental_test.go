package id3

import (
	"errors"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func TestV2ExperimentalTagsDecodeButCannotBeEdited(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		frameID string
	}{
		{version: 2, frameID: "TT2"},
		{version: 3, frameID: "TIT2"},
		{version: 4, frameID: "TIT2"},
	} {
		payload := v2TestTagVersion(test.version, 0x20, v2TestFrame(test.version, test.frameID, []byte{0, 'T'}, [2]byte{}))
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		title, ok := metadata.First(document, tag.Title())
		if !ok || title != "T" {
			t.Fatalf("experimental v2.%d = %#v", test.version, document)
		}
		builder := document.Edit()
		metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
		edited, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolver.Marshal(t.Context(), slot, "head", edited); !errors.Is(err, errV2Unsupported) {
			t.Fatalf("experimental v2.%d edit error = %v", test.version, err)
		}
	}
}
